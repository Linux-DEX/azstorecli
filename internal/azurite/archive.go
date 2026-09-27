package azurite

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// maxArchiveEntry caps a single extracted member at 8 GiB. An archive
// claiming more than that is either corrupt or a decompression bomb, and
// either way should not be allowed to fill the disk.
const maxArchiveEntry = 8 << 30

// archiveDir tars and zstd-compresses src into dst, returning the
// archive's size and the sha256 of its bytes.
func archiveDir(ctx context.Context, src, dst string, progress chan<- Progress) (int64, string, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, "", err
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, "", err
	}
	defer out.Close()

	hasher := sha256.New()
	counter := &countingWriter{}
	mw := io.MultiWriter(out, hasher, counter)

	zw, err := zstd.NewWriter(mw, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return 0, "", err
	}
	tw := tar.NewWriter(zw)

	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if snapshotSkip[d.Name()] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			// A file Azurite deleted between the walk and the stat is
			// not an error worth aborting a whole snapshot for.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		// Azurite writes only regular files and directories. Anything
		// else (socket, device, dangling symlink) would either produce
		// a header we cannot faithfully restore or smuggle a path
		// outside the workspace, so it is skipped deliberately.
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		// Ownership is meaningless across machines and leaks the
		// author's uid into a shared archive.
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		defer f.Close()

		// Copy exactly the header's byte count: tar rejects a short or
		// long write, and Azurite may be appending to an extent file
		// even with the service stopped mid-flush.
		written, err := io.Copy(tw, io.LimitReader(f, info.Size()))
		if err != nil {
			return err
		}
		if written < info.Size() {
			// File shrank under us — pad so the stream stays valid.
			if _, err := io.CopyN(tw, zeroReader{}, info.Size()-written); err != nil {
				return err
			}
		}

		report(progress, Progress{File: rel, Bytes: info.Size()})
		return nil
	})
	if walkErr != nil {
		return 0, "", walkErr
	}

	if err := tw.Close(); err != nil {
		return 0, "", err
	}
	if err := zw.Close(); err != nil {
		return 0, "", err
	}
	if err := out.Sync(); err != nil {
		return 0, "", err
	}
	report(progress, Progress{Done: true})
	return counter.n, hex.EncodeToString(hasher.Sum(nil)), nil
}

// extractArchive unpacks a snapshot into dst, sanitising every member
// path against traversal.
func extractArchive(ctx context.Context, archive, dst string, progress chan<- Progress) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	zr, err := zstd.NewReader(newCtxReader(ctx, f))
	if err != nil {
		return err
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		target, err := util.SafeJoin(dst, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size > maxArchiveEntry {
				return fmt.Errorf("archive member %q claims %d bytes; refusing to extract", hdr.Name, hdr.Size)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// Mask the archived mode: a snapshot must never be able to
			// drop a setuid or world-writable file into the workspace.
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(hdr.Mode).Perm()&0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, io.LimitReader(tr, hdr.Size)); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
			if !hdr.ModTime.IsZero() {
				_ = os.Chtimes(target, hdr.ModTime, hdr.ModTime)
			}
			report(progress, Progress{File: hdr.Name, Bytes: hdr.Size})
		default:
			// Symlinks, hardlinks, devices: archiveDir never writes
			// them, so their presence means a hand-edited archive.
			return fmt.Errorf("archive member %q has unsupported type %q", hdr.Name, string(hdr.Typeflag))
		}
	}
	report(progress, Progress{Done: true})
	return nil
}

// report sends progress without ever blocking the caller on a slow UI.
func report(ch chan<- Progress, p Progress) {
	if ch == nil {
		return
	}
	select {
	case ch <- p:
	default:
	}
}

// countingWriter totals the bytes written through it.
type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// zeroReader pads a tar entry whose file shrank mid-archive.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// ctxReader makes a long io.Copy cancellable, checked every read rather
// than only between files.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func newCtxReader(ctx context.Context, r io.Reader) io.Reader { return &ctxReader{ctx: ctx, r: r} }

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// copyFile copies src to dst atomically via a temp file.
func copyFile(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, newCtxReader(ctx, in)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// hashFile returns the sha256 and size of a file.
func hashFile(ctx context.Context, path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, newCtxReader(ctx, f))
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
