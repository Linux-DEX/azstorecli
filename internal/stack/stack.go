// Package stack is the shared runtime used by the TUI and the headless
// CLI: supervisor, workspace, profiles, storage clients, snapshots.
package stack

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/Linux-DEX/azstorecli/internal/azurite"
	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/funcs"
	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/storage"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/util"
)

// Stack owns every long-lived service for one run of azstore.
type Stack struct {
	Cfg      config.Config
	Log      *slog.Logger
	Sup      *supervisor.Supervisor
	WS       *azurite.Workspace
	Runtime  azurite.Runtime
	Version  string
	Profiles *config.ProfileStore
	Clients  *storage.Clients
	Snaps    *azurite.Store
	Keys     *keymap.KeyMap
	Theme    theme.Theme

	mu    sync.Mutex
	watch *fsnotify.Watcher
}

// Open builds a stack from a merged config. It locks the workspace so a
// second instance fails here rather than corrupting LokiJS files.
func Open(cfg config.Config) (*Stack, error) {
	if err := config.EnsureDirs(); err != nil {
		return nil, err
	}

	logger, err := openLogger()
	if err != nil {
		return nil, err
	}

	km, err := keymap.Load(config.GlobalKeymapPath())
	if err != nil {
		return nil, err
	}

	th, err := theme.Load(cfg.UI.Theme, "")
	if err != nil {
		// theme.Load already fell back to Dark; keep going with a warning.
		logger.Warn("theme", "err", err)
	}

	profiles, err := config.LoadProfiles(config.GlobalProfilesPath(), cfg)
	if err != nil {
		return nil, err
	}

	ws, err := azurite.OpenWorkspace(cfg)
	if err != nil {
		return nil, err
	}
	if err := ws.Validate(); err != nil {
		return nil, err
	}

	rt, err := azurite.Resolve(context.Background(), cfg)
	if err != nil && !errors.Is(err, azurite.ErrRuntimeUnavailable) {
		_ = ws.Close()
		return nil, err
	}
	var version string
	if err == nil {
		version, _ = azurite.DetectVersion(context.Background(), rt, cfg.Azurite.Image)
		ws.AzuriteVersion = version
	}

	sup := supervisor.New(logger)
	snaps, err := azurite.NewStore(config.SnapshotsDir(), sup, version)
	if err != nil {
		_ = ws.Close()
		return nil, err
	}

	s := &Stack{
		Cfg:      cfg,
		Log:      logger,
		Sup:      sup,
		WS:       ws,
		Runtime:  rt,
		Version:  version,
		Profiles: profiles,
		Snaps:    snaps,
		Keys:     km,
		Theme:    th,
	}
	s.RebindClients()

	if err := s.register(); err != nil {
		_ = ws.Close()
		return nil, err
	}
	_ = config.WritePID()
	return s, nil
}

func (s *Stack) register() error {
	if len(s.Runtime.Base) > 0 {
		if err := s.Sup.Register(azurite.Spec(s.Cfg, s.Runtime, s.WS)); err != nil {
			return err
		}
	}
	if s.Cfg.Functions.Enabled && funcs.IsFunctionApp(s.Cfg.FunctionAppDir()) {
		spec, err := funcs.Spec(s.Cfg)
		if err != nil && !errors.Is(err, funcs.ErrCoreToolsMissing) {
			return err
		}
		if err == nil {
			if err := s.Sup.Register(spec); err != nil {
				return err
			}
		}
	}
	return nil
}

// RebindClients rebuilds the storage facade against the active profile.
func (s *Stack) RebindClients() {
	p, ok := s.Profiles.Current()
	if !ok {
		p = s.Cfg.EmulatorProfile("azurite-local")
	}
	s.Clients = storage.New(p)
}

// LockWorkspace takes the workspace flock. Call this before starting
// Azurite; read-only commands skip it.
func (s *Stack) LockWorkspace(ctx context.Context) error {
	return s.WS.Lock(ctx, 3*time.Second)
}

// Start brings up the named services (or everything, when names is
// empty) after checking ports and taking the workspace lock.
func (s *Stack) Start(ctx context.Context, names ...string) error {
	if err := s.LockWorkspace(ctx); err != nil {
		return err
	}
	if conflicts := s.portConflicts(); len(conflicts) > 0 {
		return conflicts[0]
	}
	var err error
	if len(names) == 0 {
		err = s.Sup.StartAll(ctx)
	} else {
		var errs []error
		for _, n := range names {
			if startErr := s.Sup.Start(ctx, n); startErr != nil {
				errs = append(errs, startErr)
			}
		}
		err = errors.Join(errs...)
	}
	_ = s.saveChildPIDs()
	return err
}

// Stop halts named services, or everything when names is empty.
func (s *Stack) Stop(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		_ = ReapDetached()
		err := s.Sup.StopAll(ctx, 10*time.Second)
		clearChildPIDs()
		return err
	}
	var errs []error
	for _, n := range names {
		if err := s.Sup.Stop(ctx, n, 10*time.Second); err != nil && !errors.Is(err, supervisor.ErrForcedKill) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Restart stops and starts named services.
func (s *Stack) Restart(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		if err := s.Stop(ctx); err != nil {
			return err
		}
		return s.Start(ctx)
	}
	var errs []error
	for _, n := range names {
		if err := s.Sup.Restart(ctx, n); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Stack) portConflicts() []supervisor.PortConflict {
	ports := map[string]int{}
	if !s.Sup.IsRunning("azurite") {
		for name, port := range azurite.Ports(s.Cfg) {
			ports[name] = port
		}
	}
	if _, err := s.Sup.Get("functions"); err == nil && !s.Sup.IsRunning("functions") {
		ports["functions"] = s.Cfg.Functions.Port
	}
	return supervisor.CheckPorts(ports)
}

// Close stops children, releases the workspace, and removes the pid file.
func (s *Stack) Close() error {
	s.stopWatch()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := s.Sup.StopAll(ctx, 10*time.Second)
	s.Sup.Close()
	if wsErr := s.WS.Close(); err == nil {
		err = wsErr
	}
	config.RemovePID()
	return err
}

// StartWatch watches the function app for source changes and restarts
// the host. Debounced so a save-all does not bounce the host ten times.
func (s *Stack) StartWatch() error {
	if !s.Cfg.Functions.Watch {
		return nil
	}
	dir := s.Cfg.FunctionAppDir()
	if !funcs.IsFunctionApp(dir) {
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := addWatchRecursive(w, dir); err != nil {
		w.Close()
		return err
	}

	s.mu.Lock()
	s.watch = w
	s.mu.Unlock()

	go s.watchLoop(w)
	return nil
}

func (s *Stack) watchLoop(w *fsnotify.Watcher) {
	var timer *time.Timer
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if skipWatch(ev.Name) {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = addWatchRecursive(w, ev.Name)
				}
			}
			if timer == nil {
				timer = time.AfterFunc(400*time.Millisecond, func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_ = s.Sup.Restart(ctx, "functions")
					cancel()
				})
			} else {
				timer.Reset(400 * time.Millisecond)
			}
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

func (s *Stack) stopWatch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watch != nil {
		_ = s.watch.Close()
		s.watch = nil
	}
}

func addWatchRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if skipWatch(path) && path != root {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

func skipWatch(path string) bool {
	base := filepath.Base(path)
	switch base {
	case "node_modules", ".git", "bin", "obj", ".venv", "__pycache__",
		".azstorecli", "dist", ".vs", ".idea":
		return true
	}
	return strings.HasPrefix(base, ".")
}

func openLogger() (*slog.Logger, error) {
	path := config.LogFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > 10<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil)), nil
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})), nil
}

// InitProject writes .azstorecli/ in dir and patches local.settings.json.
func InitProject(cfg config.Config, dir string) error {
	cfg.SetProjectDir(dir)
	if err := os.MkdirAll(filepath.Join(dir, config.ProjectDirName), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Resolve(cfg.Azurite.WorkspaceDir), 0o755); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if err := azurite.EnsureGitignored(dir, cfg.Resolve(cfg.Azurite.WorkspaceDir)); err != nil {
		return err
	}
	settings, err := funcs.LoadSettings(cfg.FunctionAppDir())
	if err != nil {
		return err
	}
	if settings.EnsureDevStorage("") {
		if err := settings.Save(); err != nil {
			return err
		}
	}
	seedDir := config.ProjectSeedDir(dir)
	if err := os.MkdirAll(filepath.Join(seedDir, "fixtures"), 0o755); err != nil {
		return err
	}
	seed := filepath.Join(seedDir, "containers.yaml")
	if !util.Exists(seed) {
		body := []byte("version: 1\nblob:\n  containers: []\nqueue:\n  queues: []\ntable:\n  tables: []\n")
		if err := util.WriteAtomic(seed, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// NeedsInit reports whether dir looks like a project azstore has not
// been initialised in yet.
func NeedsInit(dir string) bool {
	if dir == "" {
		return false
	}
	if util.Exists(config.ProjectConfigPath(dir)) {
		return false
	}
	return funcs.IsFunctionApp(dir)
}
