package azurite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/util"
)

// AutosaveName is the snapshot taken automatically before a restore, so
// an accidental restore is always undoable.
const AutosaveName = "autosave-before-restore"

// Snapshot is the sidecar manifest describing one archive.
type Snapshot struct {
	ID        string    `yaml:"id"` // 2026-09-20T14-03-11_clean-seed
	Name      string    `yaml:"name"`
	CreatedAt time.Time `yaml:"createdAt"`
	Project   string    `yaml:"project"`
	SizeBytes int64     `yaml:"sizeBytes"`
	Azurite   string    `yaml:"azuriteVersion"`
	Services  []string  `yaml:"services"`
	Checksum  string    `yaml:"checksum"` // sha256 of the archive
	Notes     string    `yaml:"notes"`
}

// Archive is the path of this snapshot's compressed workspace.
func (s Snapshot) Archive(dir string) string { return filepath.Join(dir, s.ID+".tar.zst") }

// Manifest is the path of this snapshot's sidecar YAML.
func (s Snapshot) Manifest(dir string) string { return filepath.Join(dir, s.ID+".yaml") }

// VersionSkew reports whether this snapshot was made by a different
// Azurite major.minor than the one now installed. Restoring across a
// skew is allowed but warned about: LokiJS schema drift can make an
// archive load cleanly and then behave oddly.
func (s Snapshot) VersionSkew(current string) bool {
	if s.Azurite == "" || current == "" {
		return false
	}
	return majorMinor(s.Azurite) != majorMinor(current)
}

func majorMinor(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return v
	}
	return parts[0] + "." + parts[1]
}

// Progress is streamed to the UI during archive and extract.
type Progress struct {
	File  string
	Bytes int64
	Done  bool
}

// Controller is the slice of the supervisor a snapshot needs. Depending
// on an interface rather than *supervisor.Supervisor keeps the stop /
// archive / restart sequence testable without spawning a real process.
type Controller interface {
	IsRunning(name string) bool
	Stop(ctx context.Context, name string, timeout time.Duration) error
	Start(ctx context.Context, name string) error
}

// Store manages the snapshot directory.
type Store struct {
	dir     string
	sup     Controller
	version string // currently installed Azurite version, may be empty
}

// NewStore builds a snapshot store over dir.
func NewStore(dir string, sup Controller, azuriteVersion string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir, sup: sup, version: azuriteVersion}, nil
}

// Dir returns the snapshot directory.
func (s *Store) Dir() string { return s.dir }

// snapshotSkip names workspace entries that must not enter an archive:
// the lock we are standing on, and logs that only bloat it.
var snapshotSkip = map[string]bool{
	LockName:      true,
	"debug.log":   true,
	"azurite.log": true,
}

// Save stops Azurite, archives the workspace, and restarts it.
//
// The stop is not optional. Azurite's __azurite_db_*.json files are
// LokiJS databases with an autosave interval: blob payloads land in
// __blobstorage__ promptly but the metadata lags by seconds. Archiving a
// live workspace yields a torn snapshot — extents no metadata references,
// or metadata pointing at extents not yet written. It restores fine, and
// then roughly one blob in twenty is silently empty. SIGINT makes Loki
// flush on the way out, which is the only point the two agree.
func (s *Store) Save(ctx context.Context, ws *Workspace, name, notes string, progress chan<- Progress) (snap *Snapshot, err error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("snapshot needs a name")
	}
	if err := ws.Validate(); err != nil {
		return nil, err
	}

	wasRunning := s.sup != nil && s.sup.IsRunning("azurite")
	if wasRunning {
		if err := s.sup.Stop(ctx, "azurite", 10*time.Second); err != nil {
			return nil, fmt.Errorf("stop azurite before snapshot: %w", err)
		}
		defer func() {
			// context.WithoutCancel: if the user cancelled mid-snapshot
			// we still owe them a running emulator.
			if startErr := s.sup.Start(context.WithoutCancel(ctx), "azurite"); startErr != nil && err == nil {
				err = fmt.Errorf("snapshot saved but azurite failed to restart: %w", startErr)
			}
		}()
	}

	// The lock is taken after the stop: the running Azurite may have
	// been launched by this same process, which already holds it.
	if !ws.Locked() {
		if lockErr := ws.Lock(ctx, 3*time.Second); lockErr != nil {
			return nil, lockErr
		}
		defer ws.Unlock()
	}

	id := time.Now().UTC().Format("2006-01-02T15-04-05") + "_" + slug(name)
	archivePath := filepath.Join(s.dir, id+".tar.zst")
	if util.Exists(archivePath) {
		return nil, fmt.Errorf("snapshot %s already exists", id)
	}

	size, sum, archiveErr := archiveDir(ctx, ws.Dir, archivePath, progress)
	if archiveErr != nil {
		os.Remove(archivePath)
		return nil, archiveErr
	}

	version := s.version
	if version == "" {
		version = ws.AzuriteVersion
	}
	snap = &Snapshot{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC(),
		Project:   ws.Project,
		SizeBytes: size,
		Azurite:   version,
		Services:  ws.Services,
		Checksum:  sum,
		Notes:     notes,
	}
	manifest, marshalErr := yaml.Marshal(snap)
	if marshalErr != nil {
		os.Remove(archivePath)
		return nil, marshalErr
	}
	if writeErr := util.WriteAtomic(snap.Manifest(s.dir), manifest, 0o644); writeErr != nil {
		os.Remove(archivePath)
		return nil, writeErr
	}
	return snap, nil
}

// Restore replaces the workspace with a snapshot's contents.
func (s *Store) Restore(ctx context.Context, ws *Workspace, id string, progress chan<- Progress) (err error) {
	snap, err := s.Get(id)
	if err != nil {
		return err
	}

	// 1. Verify before touching anything — a corrupt archive must not
	//    cost the user their current workspace.
	if err := s.Verify(ctx, snap); err != nil {
		return err
	}

	// 2. Auto-save the current state so an accidental restore is undoable.
	if st := ws.Stat(); st.HasData {
		_ = s.Delete(AutosaveName + "-previous")
		if prev, err := s.Get(AutosaveName); err == nil {
			_ = s.rename(prev, AutosaveName+"-previous")
		}
		if _, err := s.Save(ctx, ws, AutosaveName, "automatic pre-restore copy", nil); err != nil {
			return fmt.Errorf("pre-restore autosave failed, refusing to continue: %w", err)
		}
	}

	wasRunning := s.sup != nil && s.sup.IsRunning("azurite")
	if wasRunning {
		if err := s.sup.Stop(ctx, "azurite", 10*time.Second); err != nil {
			return fmt.Errorf("stop azurite before restore: %w", err)
		}
		defer func() {
			if startErr := s.sup.Start(context.WithoutCancel(ctx), "azurite"); startErr != nil && err == nil {
				err = fmt.Errorf("restored but azurite failed to restart: %w", startErr)
			}
		}()
	}

	if !ws.Locked() {
		if lockErr := ws.Lock(ctx, 3*time.Second); lockErr != nil {
			return lockErr
		}
		defer ws.Unlock()
	}

	// 3. Empty the directory rather than removing it: the flock we hold
	//    lives on a file inside it.
	if err := util.RemoveContents(ws.Dir, map[string]bool{LockName: true}); err != nil {
		return fmt.Errorf("clear workspace: %w", err)
	}

	return extractArchive(ctx, snap.Archive(s.dir), ws.Dir, progress)
}

// List returns every snapshot, newest first.
func (s *Store) List() ([]Snapshot, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []Snapshot
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var snap Snapshot
		if err := yaml.Unmarshal(raw, &snap); err != nil || snap.ID == "" {
			continue // a manifest we cannot parse is not worth failing the list over
		}
		// A manifest whose archive vanished is a half-deleted snapshot;
		// hide it rather than offering a restore that cannot work.
		if !util.Exists(snap.Archive(s.dir)) {
			continue
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Get finds a snapshot by ID or by name. Names are not unique — the
// newest match wins, which is what "restore clean-seed" should mean.
func (s *Store) Get(idOrName string) (*Snapshot, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == idOrName {
			return &all[i], nil
		}
	}
	for i := range all {
		if all[i].Name == idOrName {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("no snapshot named %q", idOrName)
}

// Delete removes a snapshot's archive and manifest.
func (s *Store) Delete(idOrName string) error {
	snap, err := s.Get(idOrName)
	if err != nil {
		return err
	}
	// Manifest first: if the archive removal fails, List() already
	// hides the entry rather than showing a restore that would error.
	if err := os.Remove(snap.Manifest(s.dir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(snap.Archive(s.dir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Rename changes a snapshot's display name and notes in place. The ID
// (and therefore both filenames) is immutable, so nothing can break a
// reference held elsewhere.
func (s *Store) Rename(idOrName, newName, notes string) error {
	snap, err := s.Get(idOrName)
	if err != nil {
		return err
	}
	return s.rename(snap, newName, notes)
}

func (s *Store) rename(snap *Snapshot, newName string, notes ...string) error {
	if strings.TrimSpace(newName) == "" {
		return errors.New("snapshot name cannot be empty")
	}
	snap.Name = newName
	if len(notes) > 0 {
		snap.Notes = notes[0]
	}
	data, err := yaml.Marshal(snap)
	if err != nil {
		return err
	}
	return util.WriteAtomic(snap.Manifest(s.dir), data, 0o644)
}

// Verify recomputes the archive checksum and compares it to the
// manifest.
func (s *Store) Verify(ctx context.Context, snap *Snapshot) error {
	f, err := os.Open(snap.Archive(s.dir))
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, newCtxReader(ctx, f)); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if snap.Checksum != "" && got != snap.Checksum {
		return fmt.Errorf("checksum mismatch for %s: archive is corrupt or was modified", snap.ID)
	}
	return nil
}

// Prune deletes snapshots older than the cutoff, always keeping the
// autosave so the undo path survives a prune.
func (s *Store) Prune(olderThan time.Duration) ([]string, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-olderThan)

	var removed []string
	var errs []error
	for _, snap := range all {
		if strings.HasPrefix(snap.Name, AutosaveName) || !snap.CreatedAt.Before(cutoff) {
			continue
		}
		if err := s.Delete(snap.ID); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, snap.ID)
	}
	return removed, errors.Join(errs...)
}

// Export copies a snapshot's archive to an arbitrary path, writing the
// manifest alongside it so an imported copy keeps its provenance.
func (s *Store) Export(ctx context.Context, idOrName, dst string) error {
	snap, err := s.Get(idOrName)
	if err != nil {
		return err
	}
	if err := s.Verify(ctx, snap); err != nil {
		return err
	}
	if filepath.Ext(dst) == "" {
		dst = filepath.Join(dst, snap.ID+".tar.zst")
	}
	if err := copyFile(ctx, snap.Archive(s.dir), dst); err != nil {
		return err
	}
	manifest, err := yaml.Marshal(snap)
	if err != nil {
		return err
	}
	return util.WriteAtomic(strings.TrimSuffix(dst, ".tar.zst")+".yaml", manifest, 0o644)
}

// Import brings an exported archive into the store, rebuilding the
// manifest from the sidecar if one shipped with it and synthesising one
// if not.
func (s *Store) Import(ctx context.Context, src string) (*Snapshot, error) {
	if !strings.HasSuffix(src, ".tar.zst") {
		return nil, fmt.Errorf("%s does not look like a snapshot archive", src)
	}
	base := strings.TrimSuffix(filepath.Base(src), ".tar.zst")

	var snap Snapshot
	if raw, err := os.ReadFile(strings.TrimSuffix(src, ".tar.zst") + ".yaml"); err == nil {
		_ = yaml.Unmarshal(raw, &snap)
	}
	if snap.ID == "" {
		info, err := os.Stat(src)
		if err != nil {
			return nil, err
		}
		snap = Snapshot{
			ID:        time.Now().UTC().Format("2006-01-02T15-04-05") + "_" + slug(base),
			Name:      base,
			CreatedAt: info.ModTime().UTC(),
			SizeBytes: info.Size(),
			Notes:     "imported from " + src,
		}
	}
	// A collision means importing the same archive twice; give the copy
	// a fresh ID rather than silently overwriting.
	if util.Exists(snap.Archive(s.dir)) {
		snap.ID = time.Now().UTC().Format("2006-01-02T15-04-05") + "_" + slug(snap.Name)
	}

	if err := copyFile(ctx, src, snap.Archive(s.dir)); err != nil {
		return nil, err
	}
	// Recompute rather than trusting the sidecar: an archive that was
	// truncated in transit must fail here, not at restore time.
	sum, size, err := hashFile(ctx, snap.Archive(s.dir))
	if err != nil {
		os.Remove(snap.Archive(s.dir))
		return nil, err
	}
	if snap.Checksum != "" && snap.Checksum != sum {
		os.Remove(snap.Archive(s.dir))
		return nil, fmt.Errorf("imported archive does not match its manifest checksum")
	}
	snap.Checksum, snap.SizeBytes = sum, size

	data, err := yaml.Marshal(&snap)
	if err != nil {
		os.Remove(snap.Archive(s.dir))
		return nil, err
	}
	if err := util.WriteAtomic(snap.Manifest(s.dir), data, 0o644); err != nil {
		os.Remove(snap.Archive(s.dir))
		return nil, err
	}
	return &snap, nil
}

// TotalSize sums every archive in the store.
func (s *Store) TotalSize() int64 {
	all, _ := s.List()
	var total int64
	for _, snap := range all {
		total += snap.SizeBytes
	}
	return total
}

// DefaultStoreDir is the snapshot directory under XDG data home.
func DefaultStoreDir() string { return config.SnapshotsDir() }
