package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// State is the lifecycle position of one supervised process.
type State int32

const (
	StateStopped State = iota
	StateStarting
	StateHealthy
	StateUnhealthy
	StateCrashed
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateHealthy:
		return "healthy"
	case StateUnhealthy:
		return "unhealthy"
	case StateCrashed:
		return "crashed"
	default:
		return "stopped"
	}
}

// Running reports whether a process in this state has a live PID.
func (s State) Running() bool {
	return s == StateStarting || s == StateHealthy || s == StateUnhealthy
}

// RestartPolicy controls what happens when a process exits on its own.
type RestartPolicy int

const (
	RestartNever RestartPolicy = iota
	RestartOnFailure
	RestartAlways
)

var (
	// ErrAlreadyRunning is returned by Start on a live process.
	ErrAlreadyRunning = errors.New("process is already running")
	// ErrForcedKill reports that the graceful stop timeout elapsed and
	// the tree had to be SIGKILLed.
	ErrForcedKill = errors.New("process did not exit gracefully and was killed")
	// ErrNotRegistered names a process the supervisor does not know.
	ErrNotRegistered = errors.New("no such process")
)

// Spec is the declarative description of a supervised process.
type Spec struct {
	Name        string
	Argv        []string
	Dir         string
	Env         []string // appended to os.Environ()
	DependsOn   []string
	Health      HealthCheck
	Restart     RestartPolicy
	MaxRestarts int
	StopSignal  os.Signal
	StopTimeout time.Duration
	LogCapacity int
}

func (s Spec) withDefaults() Spec {
	if s.StopSignal == nil {
		s.StopSignal = DefaultStopSignal
	}
	if s.StopTimeout <= 0 {
		s.StopTimeout = 10 * time.Second
	}
	if s.LogCapacity <= 0 {
		s.LogCapacity = 10_000
	}
	if s.MaxRestarts <= 0 {
		s.MaxRestarts = 3
	}
	return s
}

// Process wraps one exec.Cmd with log capture, health monitoring, and a
// restart policy.
type Process struct {
	spec  Spec
	logs  *LogRing
	emit  func(Event)
	state atomic.Int32

	mu           sync.Mutex
	cmd          *exec.Cmd
	pipes        []io.Closer
	pumps        sync.WaitGroup
	exited       chan struct{} // closed once cmd.Wait returns
	exitErr      error
	stopping     bool // a deliberate Stop is in flight; suppress restart
	startedAt    time.Time
	healthySince time.Time
	restarts     int
	lastErr      error
	monitorStop  context.CancelFunc
}

func newProcess(spec Spec, emit func(Event)) *Process {
	spec = spec.withDefaults()
	return &Process{
		spec: spec,
		logs: NewLogRing(spec.LogCapacity),
		emit: emit,
	}
}

// State reports the current lifecycle state.
func (p *Process) State() State { return State(p.state.Load()) }

// Logs returns a chronological copy of the captured output.
func (p *Process) Logs() *LogRing { return p.logs }

// Spec returns the process specification.
func (p *Process) Spec() Spec { return p.spec }

func (p *Process) setState(s State, err error) {
	prev := State(p.state.Swap(int32(s)))
	if s == StateHealthy && prev != StateHealthy {
		p.mu.Lock()
		p.healthySince = time.Now()
		p.mu.Unlock()
	}
	if prev == s && err == nil {
		return
	}
	p.emit(Event{
		Kind:    EventState,
		Process: p.spec.Name,
		State:   s,
		PID:     p.PID(),
		Err:     err,
		At:      time.Now(),
	})
}

// PID returns the child's process ID, or 0 when it is not running.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Uptime reports how long the current run has lasted.
func (p *Process) Uptime() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.startedAt.IsZero() || !p.State().Running() {
		return 0
	}
	return time.Since(p.startedAt)
}

// LastError returns the most recent failure, for the dashboard detail
// pane.
func (p *Process) LastError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

// Start spawns the process and blocks until its health check passes.
// Callers that must not block (the TUI) should run it inside a tea.Cmd.
func (p *Process) Start(ctx context.Context) error {
	if err := p.spawn(); err != nil {
		return err
	}

	if p.spec.Health == nil {
		p.setState(StateHealthy, nil)
		return nil
	}

	// Race the health probe against the process dying outright: a child
	// that exits in 200ms should surface its error immediately rather
	// than after the full 15-probe budget.
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-exited:
			cancel()
		case <-probeCtx.Done():
		}
	}()

	if err := WaitHealthy(probeCtx, p.spec.Health); err != nil {
		select {
		case <-exited:
			p.mu.Lock()
			exitErr := p.exitErr
			p.mu.Unlock()
			err = fmt.Errorf("%s exited during startup: %w", p.spec.Name, exitErr)
		default:
		}
		p.recordErr(err)
		p.setState(StateUnhealthy, err)
		return err
	}

	p.setState(StateHealthy, nil)
	p.startMonitor()
	return nil
}

// spawn does the exec.Cmd work under the lock and returns as soon as the
// child is running.
func (p *Process) spawn() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.State().Running() {
		return ErrAlreadyRunning
	}
	if len(p.spec.Argv) == 0 {
		return fmt.Errorf("%s: empty argv", p.spec.Name)
	}

	cmd := exec.Command(p.spec.Argv[0], p.spec.Argv[1:]...)
	cmd.Dir = p.spec.Dir
	cmd.Env = append(os.Environ(), p.spec.Env...)
	configureProcAttr(cmd)

	// Real os.Pipe file descriptors rather than cmd.StdoutPipe: with a
	// *os.File, exec hands the fd straight to the child and cmd.Wait
	// returns the moment the process exits. With a plain io.Writer,
	// Wait also blocks on exec's copy goroutines, which never finish
	// while an orphaned grandchild still holds the write end.
	outR, outW, err := os.Pipe()
	if err != nil {
		return err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return err
	}
	cmd.Stdout, cmd.Stderr = outW, errW

	if err := cmd.Start(); err != nil {
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
		p.lastErr = err
		p.state.Store(int32(StateCrashed))
		return fmt.Errorf("start %s: %w", p.spec.Name, err)
	}

	// The child holds its own dup of the write ends; drop ours so the
	// reader sees EOF when the tree is gone.
	outW.Close()
	errW.Close()

	p.cmd = cmd
	p.pipes = []io.Closer{outR, errR}
	p.exited = make(chan struct{})
	p.exitErr = nil
	p.stopping = false
	p.startedAt = time.Now()
	p.state.Store(int32(StateStarting))

	p.pumps.Add(2)
	go p.pump(outR, Stdout)
	go p.pump(errR, Stderr)

	go p.reap(cmd, p.exited)

	p.emit(Event{
		Kind: EventState, Process: p.spec.Name,
		State: StateStarting, PID: cmd.Process.Pid, At: time.Now(),
	})
	return nil
}

// pump scans one stream into the ring buffer and notifies subscribers.
func (p *Process) pump(r io.ReadCloser, stream Stream) {
	defer p.pumps.Done()
	defer r.Close()

	sc := bufio.NewScanner(r)
	// Functions host stack traces and Azurite request dumps routinely
	// exceed bufio's 64KB default, which would abort the scan mid-stream.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := p.logs.Append(LogLine{
			At:      time.Now(),
			Process: p.spec.Name,
			Stream:  stream,
			Text:    sc.Text(),
		})
		p.emit(Event{Kind: EventLog, Process: p.spec.Name, Line: line, At: line.At})
	}
	if err := sc.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
		p.logs.Append(LogLine{
			At: time.Now(), Process: p.spec.Name, Stream: Stderr,
			Text: "[azstore] log capture stopped: " + err.Error(),
		})
	}
}

// reap waits for the child, drains the pipes, and applies the restart
// policy. Exactly one goroutine per run calls cmd.Wait.
func (p *Process) reap(cmd *exec.Cmd, exited chan struct{}) {
	waitErr := cmd.Wait()

	// The tree is gone; give the pumps a moment to drain whatever is
	// still buffered, then force the read ends closed so they cannot
	// hang on a grandchild that inherited the write end.
	drained := make(chan struct{})
	go func() { p.pumps.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		p.mu.Lock()
		pipes := p.pipes
		p.mu.Unlock()
		for _, c := range pipes {
			_ = c.Close()
		}
		<-drained
	}

	p.mu.Lock()
	p.exitErr = waitErr
	deliberate := p.stopping
	if p.monitorStop != nil {
		p.monitorStop()
		p.monitorStop = nil
	}
	p.mu.Unlock()
	close(exited)

	if deliberate {
		p.setState(StateStopped, nil)
		return
	}

	if waitErr != nil {
		p.recordErr(waitErr)
	}
	p.setState(StateCrashed, waitErr)
	p.maybeRestart(waitErr)
}

// maybeRestart applies the restart policy with exponential backoff.
func (p *Process) maybeRestart(exitErr error) {
	switch p.spec.Restart {
	case RestartNever:
		return
	case RestartOnFailure:
		if exitErr == nil {
			return
		}
	}

	p.mu.Lock()
	// A process that ran healthy for a while earned a clean slate — this
	// is a crash-loop brake, not a lifetime quota.
	if !p.healthySince.IsZero() && time.Since(p.healthySince) > time.Minute {
		p.restarts = 0
	}
	if p.restarts >= p.spec.MaxRestarts {
		p.mu.Unlock()
		err := fmt.Errorf("%s crashed %d times, giving up", p.spec.Name, p.restarts)
		p.recordErr(err)
		p.emit(Event{Kind: EventError, Process: p.spec.Name, Err: err, At: time.Now()})
		return
	}
	p.restarts++
	attempt := p.restarts
	p.mu.Unlock()

	delay := util.Backoff(attempt-1, time.Second, 30*time.Second)
	p.emit(Event{
		Kind:    EventError,
		Process: p.spec.Name,
		Err:     fmt.Errorf("restarting %s in %s (attempt %d/%d)", p.spec.Name, delay, attempt, p.spec.MaxRestarts),
		At:      time.Now(),
	})

	time.Sleep(delay)

	p.mu.Lock()
	aborted := p.stopping || p.State().Running()
	p.mu.Unlock()
	if aborted {
		return
	}
	if err := p.Start(context.Background()); err != nil {
		p.recordErr(err)
	}
}

// startMonitor keeps probing after startup so a service that wedges
// mid-run shows as unhealthy rather than staying green on a live PID.
func (p *Process) startMonitor() {
	if p.spec.Health == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())

	p.mu.Lock()
	if p.monitorStop != nil {
		p.monitorStop()
	}
	p.monitorStop = cancel
	p.mu.Unlock()

	go func() {
		t := time.NewTicker(p.spec.Health.Policy().Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !p.State().Running() {
					return
				}
				err := p.spec.Health.Probe(ctx)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					p.recordErr(err)
					p.setState(StateUnhealthy, err)
				} else {
					p.setState(StateHealthy, nil)
				}
			}
		}
	}()
}

// Stop signals the process tree and waits for it to exit, escalating to
// SIGKILL after timeout. Stopping an already-stopped process is a no-op,
// not an error — callers routinely stop defensively.
func (p *Process) Stop(ctx context.Context, timeout time.Duration) error {
	p.mu.Lock()
	if p.cmd == nil || p.cmd.Process == nil || !p.State().Running() {
		p.mu.Unlock()
		return nil
	}
	p.stopping = true
	if p.monitorStop != nil {
		p.monitorStop()
		p.monitorStop = nil
	}
	proc := p.cmd.Process
	exited := p.exited
	if timeout <= 0 {
		timeout = p.spec.StopTimeout
	}
	p.mu.Unlock()

	if err := signalTree(proc, p.spec.StopSignal); err != nil {
		// Already reaped between the state check and the signal.
		select {
		case <-exited:
			return nil
		default:
		}
	}

	select {
	case <-exited:
		return nil
	case <-time.After(timeout):
	case <-ctx.Done():
		// Caller gave up waiting, but we must not leave the tree behind.
	}

	_ = killTree(proc)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("%s ignored SIGKILL", p.spec.Name)
	}
	return ErrForcedKill
}

// Wait blocks until the current run exits.
func (p *Process) Wait(ctx context.Context) error {
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()
	if exited == nil {
		return nil
	}
	select {
	case <-exited:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.exitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Process) recordErr(err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	p.lastErr = err
	p.mu.Unlock()
}
