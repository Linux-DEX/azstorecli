package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

// blockSize is the chunk each upload/download worker moves. 4 MiB is
// the SDK's own sweet spot and keeps progress updates frequent enough
// to look live without flooding the render loop.
const blockSize = 4 * 1024 * 1024

// Transfer reports the progress of one file in a multi-file operation.
type Transfer struct {
	Name    string
	Total   int64
	Done    int64
	Index   int
	Count   int
	Err     error
	Skipped bool
}

// Upload sends one local file to a blob, streaming progress.
func (c *Clients) Upload(ctx context.Context, containerName, blobName, localPath string, progress chan<- Transfer) error {
	if err := c.Guard(); err != nil {
		return err
	}
	client, err := c.Blob()
	if err != nil {
		return err
	}

	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; use UploadDir", localPath)
	}

	var moved atomic.Int64
	reader := &progressReader{
		r: f,
		onRead: func(n int) {
			total := moved.Add(int64(n))
			send(progress, Transfer{Name: blobName, Total: info.Size(), Done: total})
		},
	}

	_, err = client.UploadStream(ctx, containerName, blobName, reader, &azblob.UploadStreamOptions{
		BlockSize:   blockSize,
		Concurrency: 4,
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: ptr(GuessContentType(blobName))},
	})
	if err != nil {
		return wrapAzure("upload "+blobName, err)
	}
	send(progress, Transfer{Name: blobName, Total: info.Size(), Done: info.Size()})
	return nil
}

// UploadDir recursively uploads a directory, mapping the local tree onto
// blob prefixes under destPrefix.
func (c *Clients) UploadDir(ctx context.Context, containerName, destPrefix, localDir string, progress chan<- Transfer) error {
	if err := c.Guard(); err != nil {
		return err
	}

	var files []string
	err := filepath.WalkDir(localDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Symlinks are not followed: a link pointing outside the chosen
		// directory would silently upload files the user never selected.
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}

	var errs []error
	for i, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		name := JoinPrefix(destPrefix, filepath.ToSlash(rel))

		if err := c.Upload(ctx, containerName, name, path, nil); err != nil {
			// One unreadable file must not abandon the other 200.
			errs = append(errs, fmt.Errorf("%s: %w", rel, err))
			send(progress, Transfer{Name: name, Index: i + 1, Count: len(files), Err: err})
			continue
		}
		send(progress, Transfer{Name: name, Index: i + 1, Count: len(files)})
	}
	return errors.Join(errs...)
}

// Download fetches one blob to a local path with progress.
func (c *Clients) Download(ctx context.Context, containerName, blobName, localPath string, progress chan<- Transfer) error {
	bc, err := c.blobClient(containerName, blobName)
	if err != nil {
		return err
	}
	resp, err := bc.DownloadStream(ctx, nil)
	if err != nil {
		return wrapAzure("download "+blobName, err)
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	// Write to a temp file and rename: an interrupted download must not
	// leave something at the destination that looks like a finished file.
	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".azstore-dl-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	total := deref(resp.ContentLength)
	var moved atomic.Int64
	reader := &progressReader{
		r: resp.NewRetryReader(ctx, &blob.RetryReaderOptions{MaxRetries: 3}),
		onRead: func(n int) {
			send(progress, Transfer{Name: blobName, Total: total, Done: moved.Add(int64(n))})
		},
	}

	if _, err := io.Copy(tmp, reader); err != nil {
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
	return os.Rename(tmp.Name(), localPath)
}

// DownloadMany fetches a set of blobs into a directory, preserving
// their prefix structure.
func (c *Clients) DownloadMany(ctx context.Context, containerName string, blobNames []string, destDir string, progress chan<- Transfer) error {
	var errs []error
	for i, name := range blobNames {
		if err := ctx.Err(); err != nil {
			return err
		}
		// SafeJoin semantics: a blob literally named "../escape" must
		// not write outside the chosen directory.
		dst := filepath.Join(destDir, filepath.FromSlash(sanitizeBlobPath(name)))
		if !strings.HasPrefix(dst, filepath.Clean(destDir)+string(os.PathSeparator)) {
			errs = append(errs, fmt.Errorf("%s: refusing to write outside %s", name, destDir))
			continue
		}
		if err := c.Download(ctx, containerName, name, dst, nil); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			send(progress, Transfer{Name: name, Index: i + 1, Count: len(blobNames), Err: err})
			continue
		}
		send(progress, Transfer{Name: name, Index: i + 1, Count: len(blobNames)})
	}
	return errors.Join(errs...)
}

// sanitizeBlobPath strips traversal segments from a blob name so it can
// be used as a relative local path.
func sanitizeBlobPath(name string) string {
	parts := strings.Split(name, "/")
	clean := parts[:0]
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		clean = append(clean, p)
	}
	if len(clean) == 0 {
		return "blob"
	}
	return strings.Join(clean, "/")
}

// progressReader reports bytes as they pass through.
type progressReader struct {
	r      io.Reader
	onRead func(int)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 && p.onRead != nil {
		p.onRead(n)
	}
	return n, err
}

// send never blocks the transfer on a slow UI.
func send(ch chan<- Transfer, t Transfer) {
	if ch == nil {
		return
	}
	select {
	case ch <- t:
	default:
	}
}
