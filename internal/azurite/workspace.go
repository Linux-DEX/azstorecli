package azurite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/util"
)

// LockName is the advisory lock file inside a workspace. Two azstore
// instances pointed at one workspace would corrupt the LokiJS databases,
// so the lock is taken for the whole time Azurite is running.
const LockName = ".azstore.lock"

// dataFiles are the artefacts Azurite creates in a workspace. Cleaning
// the emulator means removing exactly these, not the directory itself —
// the directory holds the lock we are standing on.
var dataFiles = []string{
	"__azurite_db_blob__.json",
	"__azurite_db_blob_extent__.json",
	"__blobstorage__",
	"__azurite_db_queue__.json",
	"__azurite_db_queue_extent__.json",
	"__queuestorage__",
	"__azurite_db_table__.json",
	"__azurite_db_table_extent__.json",
	"__tablestorage__",
}

// ErrWorkspaceBusy means another process holds the workspace lock.
var ErrWorkspaceBusy = errors.New("workspace is locked by another process")

// Workspace is a directory Azurite persists into, plus the lock that
// keeps a single owner.
type Workspace struct {
	Dir            string
	Project        string
	Services       []string
	AzuriteVersion string
	Ephemeral      bool // temp mode: removed on Close

	mu   sync.Mutex
	lock *flock.Flock
	held bool
}

// OpenWorkspace resolves the configured workspace mode into a directory
// and creates it. It does not take the lock — call Lock separately, so
// read-only commands (`snapshot list`) never block on a running TUI.
func OpenWorkspace(cfg config.Config) (*Workspace, error) {
	var dir string
	var ephemeral bool

	switch cfg.Azurite.WorkspaceMode {
	case "project":
		dir = cfg.Resolve(cfg.Azurite.WorkspaceDir)
	case "global":
		name := cfg.Project.Name
		if name == "" {
			name = "default"
		}
		dir = filepath.Join(config.WorkspacesDir(), slug(name))
	case "temp":
		d, err := os.MkdirTemp("", "azstore-ws-*")
		if err != nil {
			return nil, err
		}
		dir, ephemeral = d, true
	default:
		return nil, fmt.Errorf("unknown workspaceMode %q", cfg.Azurite.WorkspaceMode)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace %s: %w", dir, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	return &Workspace{
		Dir:       abs,
		Project:   cfg.Project.Name,
		Services:  append([]string{}, cfg.Azurite.Services...),
		Ephemeral: ephemeral,
		lock:      flock.New(filepath.Join(abs, LockName)),
	}, nil
}

// Lock takes the workspace lock, waiting up to timeout. Failing to
// acquire it is an expected outcome (a second azstore is already
// running here), not an internal error.
func (w *Workspace) Lock(ctx context.Context, timeout time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.held {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ok, err := w.lock.TryLockContext(ctx, 200*time.Millisecond)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %s", ErrWorkspaceBusy, w.Dir)
		}
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrWorkspaceBusy, w.Dir)
	}
	w.held = true
	return nil
}

// Unlock releases the workspace lock if held.
func (w *Workspace) Unlock() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.held {
		return nil
	}
	w.held = false
	return w.lock.Unlock()
}

// Locked reports whether this process holds the lock.
func (w *Workspace) Locked() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.held
}

// Close releases the lock and removes the directory if it was temporary.
func (w *Workspace) Close() error {
	err := w.Unlock()
	if w.Ephemeral {
		if rmErr := os.RemoveAll(w.Dir); rmErr != nil && err == nil {
			err = rmErr
		}
	}
	return err
}

// Clean removes Azurite's data files so the emulator starts empty. Only
// the known artefacts are deleted: a project workspace may sit inside a
// directory the user also keeps notes in, and a blind RemoveAll there
// would be unrecoverable.
func (w *Workspace) Clean() error {
	var errs []error
	for _, name := range dataFiles {
		if err := os.RemoveAll(filepath.Join(w.Dir, name)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Stats summarises what is on disk, for the dashboard workspace tile.
type Stats struct {
	SizeBytes int64
	HasData   bool
	Modified  time.Time
}

// Stat reports the workspace's size and whether Azurite has written to
// it yet.
func (w *Workspace) Stat() Stats {
	s := Stats{SizeBytes: util.DirSize(w.Dir)}
	for _, name := range dataFiles {
		info, err := os.Stat(filepath.Join(w.Dir, name))
		if err != nil {
			continue
		}
		s.HasData = true
		if info.ModTime().After(s.Modified) {
			s.Modified = info.ModTime()
		}
	}
	return s
}

// Validate checks that the directory is usable before Azurite is
// spawned — a permission or file-not-directory problem reported here is
// far clearer than the child's stderr three seconds later.
func (w *Workspace) Validate() error {
	info, err := os.Stat(w.Dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", w.Dir)
	}
	probe := filepath.Join(w.Dir, ".azstore-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("workspace %s is not writable: %w", w.Dir, err)
	}
	return os.Remove(probe)
}

// EnsureGitignored appends the workspace to the project .gitignore so
// live emulator data never lands in a commit. It is a no-op when the
// pattern is already present or when there is no git repo.
func EnsureGitignored(projectDir, workspaceDir string) error {
	rel, err := filepath.Rel(projectDir, workspaceDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil // workspace lives outside the project; nothing to ignore
	}
	pattern := filepath.ToSlash(rel) + "/"

	path := filepath.Join(projectDir, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}

	body := string(existing)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "\n# azstorecli: live Azurite data\n" + pattern + "\n"
	return util.WriteAtomic(path, []byte(body), 0o644)
}

// slug makes a string safe for use as a directory or archive name.
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unnamed"
	}
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-")
	}
	return out
}
