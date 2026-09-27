package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// EventKind tags what a supervisor Event reports.
type EventKind int

const (
	EventState EventKind = iota
	EventLog
	EventError
)

// Event is the single notification type the supervisor publishes.
type Event struct {
	Kind    EventKind
	Process string
	State   State
	PID     int
	Line    LogLine
	Err     error
	At      time.Time
}

// Status is a point-in-time summary of one process, for the dashboard
// and for `azstore status --json`.
type Status struct {
	Name      string        `json:"name"`
	State     string        `json:"state"`
	PID       int           `json:"pid"`
	Uptime    time.Duration `json:"uptimeNs"`
	Restarts  int           `json:"restarts"`
	LastError string        `json:"lastError,omitempty"`
	Argv      []string      `json:"argv"`
	Health    string        `json:"health,omitempty"`
	DependsOn []string      `json:"dependsOn,omitempty"`
}

// Supervisor owns the registry of managed processes and the ordering
// rules between them.
type Supervisor struct {
	mu    sync.RWMutex
	procs map[string]*Process
	order []string // registration order, used as a tiebreak in the topo sort

	events  chan Event
	dropped atomic.Uint64
	log     *slog.Logger

	closeOnce sync.Once
}

// New builds an empty supervisor. The events channel is generously
// buffered and lossy on overflow: correctness never depends on a
// subscriber keeping up, because Snapshot() is the source of truth for
// state and LogRing.Since() is the source of truth for output.
func New(logger *slog.Logger) *Supervisor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{
		procs:  make(map[string]*Process),
		events: make(chan Event, 4096),
		log:    logger,
	}
}

// Events returns the read side of the notification stream.
func (s *Supervisor) Events() <-chan Event { return s.events }

// Dropped reports how many events were discarded because the channel
// was full — surfaced in the logs screen so a stall is visible.
func (s *Supervisor) Dropped() uint64 { return s.dropped.Load() }

func (s *Supervisor) emit(e Event) {
	select {
	case s.events <- e:
	default:
		// Never block a log pump on a slow render loop.
		s.dropped.Add(1)
	}
}

// Register adds a process spec. Re-registering a name replaces the spec
// only if that process is stopped, so a config reload cannot swap the
// argv out from under a running child.
func (s *Supervisor) Register(spec Spec) error {
	if spec.Name == "" {
		return errors.New("supervisor: spec needs a name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.procs[spec.Name]; ok {
		if existing.State().Running() {
			return fmt.Errorf("cannot redefine %q while it is running", spec.Name)
		}
		s.procs[spec.Name] = newProcess(spec, s.emit)
		return nil
	}
	s.procs[spec.Name] = newProcess(spec, s.emit)
	s.order = append(s.order, spec.Name)
	return nil
}

// Get returns a registered process.
func (s *Supervisor) Get(name string) (*Process, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.procs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotRegistered, name)
	}
	return p, nil
}

// Names returns registered process names in registration order.
func (s *Supervisor) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// IsRunning reports whether a process currently has a live PID.
func (s *Supervisor) IsRunning(name string) bool {
	p, err := s.Get(name)
	return err == nil && p.State().Running()
}

// State returns a process's state, or StateStopped if it is unknown.
func (s *Supervisor) State(name string) State {
	p, err := s.Get(name)
	if err != nil {
		return StateStopped
	}
	return p.State()
}

// Logs returns a process's log ring.
func (s *Supervisor) Logs(name string) (*LogRing, error) {
	p, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	return p.Logs(), nil
}

// Snapshot summarises every registered process. This is what the UI
// polls, so a dropped event only ever delays a repaint.
func (s *Supervisor) Snapshot() []Status {
	s.mu.RLock()
	names := make([]string, len(s.order))
	copy(names, s.order)
	procs := make([]*Process, 0, len(names))
	for _, n := range names {
		procs = append(procs, s.procs[n])
	}
	s.mu.RUnlock()

	out := make([]Status, 0, len(procs))
	for _, p := range procs {
		st := Status{
			Name:      p.spec.Name,
			State:     p.State().String(),
			PID:       p.PID(),
			Uptime:    p.Uptime(),
			Argv:      p.spec.Argv,
			DependsOn: p.spec.DependsOn,
		}
		p.mu.Lock()
		st.Restarts = p.restarts
		p.mu.Unlock()
		if err := p.LastError(); err != nil {
			st.LastError = err.Error()
		}
		if p.spec.Health != nil {
			st.Health = p.spec.Health.Describe()
		}
		out = append(out, st)
	}
	return out
}

// Start brings up one process, starting any dependency that is not
// already healthy first.
func (s *Supervisor) Start(ctx context.Context, name string) error {
	plan, err := s.resolve([]string{name})
	if err != nil {
		return err
	}
	return s.startSequence(ctx, plan)
}

// StartAll brings up every registered process in dependency order.
func (s *Supervisor) StartAll(ctx context.Context) error {
	plan, err := s.resolve(s.Names())
	if err != nil {
		return err
	}
	return s.startSequence(ctx, plan)
}

func (s *Supervisor) startSequence(ctx context.Context, plan []string) error {
	for _, name := range plan {
		p, err := s.Get(name)
		if err != nil {
			return err
		}
		if p.State() == StateHealthy {
			continue
		}
		if p.State().Running() {
			// Starting or unhealthy: wait for it rather than spawning a
			// second copy that will fight over the same port.
			if p.spec.Health != nil {
				if err := WaitHealthy(ctx, p.spec.Health); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
			continue
		}

		s.log.Info("starting process", "name", name, "argv", p.spec.Argv)
		if err := p.Start(ctx); err != nil {
			// A dependent that cannot start makes everything after it
			// pointless, and leaving half a stack up is worse than none.
			return fmt.Errorf("start %s: %w", name, err)
		}
	}
	return nil
}

// Stop halts one process. Anything that depends on it is stopped first,
// because a Functions host outliving its storage backend just spews
// connection errors.
func (s *Supervisor) Stop(ctx context.Context, name string, timeout time.Duration) error {
	if _, err := s.Get(name); err != nil {
		return err
	}
	for _, dep := range s.dependents(name) {
		p, err := s.Get(dep)
		if err != nil {
			continue
		}
		if err := p.Stop(ctx, timeout); err != nil && !errors.Is(err, ErrForcedKill) {
			s.log.Warn("stopping dependent failed", "name", dep, "err", err)
		}
	}
	p, err := s.Get(name)
	if err != nil {
		return err
	}
	return p.Stop(ctx, timeout)
}

// StopAll halts everything in reverse dependency order.
func (s *Supervisor) StopAll(ctx context.Context, timeout time.Duration) error {
	plan, err := s.resolve(s.Names())
	if err != nil {
		// A dependency cycle must not prevent shutdown; fall back to
		// registration order reversed.
		plan = s.Names()
	}
	var errs []error
	for i := len(plan) - 1; i >= 0; i-- {
		p, err := s.Get(plan[i])
		if err != nil {
			continue
		}
		if err := p.Stop(ctx, timeout); err != nil && !errors.Is(err, ErrForcedKill) {
			errs = append(errs, fmt.Errorf("%s: %w", plan[i], err))
		}
	}
	return errors.Join(errs...)
}

// Restart stops and starts a process, preserving dependency ordering.
func (s *Supervisor) Restart(ctx context.Context, name string) error {
	p, err := s.Get(name)
	if err != nil {
		return err
	}
	if err := p.Stop(ctx, p.spec.StopTimeout); err != nil && !errors.Is(err, ErrForcedKill) {
		return err
	}
	return s.Start(ctx, name)
}

// Close shuts the event channel. Call it once, after StopAll.
func (s *Supervisor) Close() {
	s.closeOnce.Do(func() { close(s.events) })
}

// dependents lists every registered process that transitively depends on
// name, nearest-first.
func (s *Supervisor) dependents(name string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []string
	seen := map[string]bool{name: true}
	frontier := []string{name}
	for len(frontier) > 0 {
		cur := frontier[0]
		frontier = frontier[1:]
		for _, candidate := range s.order {
			if seen[candidate] {
				continue
			}
			for _, dep := range s.procs[candidate].spec.DependsOn {
				if dep == cur {
					seen[candidate] = true
					out = append(out, candidate)
					frontier = append(frontier, candidate)
					break
				}
			}
		}
	}
	return out
}

// resolve topologically sorts the requested processes together with
// their transitive dependencies, so callers get a valid start order.
func (s *Supervisor) resolve(roots []string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const (
		white = 0 // unvisited
		grey  = 1 // on the current DFS path
		black = 2 // fully expanded
	)
	color := make(map[string]int, len(s.procs))
	var out []string
	var path []string

	var visit func(string) error
	visit = func(name string) error {
		switch color[name] {
		case black:
			return nil
		case grey:
			// Report the actual loop, not just "a cycle exists".
			loop := append(append([]string{}, path...), name)
			return fmt.Errorf("dependency cycle: %v", loop)
		}
		if _, ok := s.procs[name]; !ok {
			return fmt.Errorf("%w: %q", ErrNotRegistered, name)
		}

		color[name] = grey
		path = append(path, name)

		deps := append([]string{}, s.procs[name].spec.DependsOn...)
		sort.Strings(deps) // deterministic plans make failures reproducible
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}

		path = path[:len(path)-1]
		color[name] = black
		out = append(out, name)
		return nil
	}

	for _, r := range roots {
		if err := visit(r); err != nil {
			return nil, err
		}
	}
	return out, nil
}
