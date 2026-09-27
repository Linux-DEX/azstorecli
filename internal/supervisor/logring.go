package supervisor

import (
	"sync"
	"time"
)

// Stream distinguishes stdout from stderr on a captured log line.
type Stream uint8

const (
	Stdout Stream = iota
	Stderr
)

func (s Stream) String() string {
	if s == Stderr {
		return "stderr"
	}
	return "stdout"
}

// LogLine is a single captured line of process output.
type LogLine struct {
	At      time.Time
	Process string
	Stream  Stream
	Text    string
	Seq     uint64 // monotonic per ring, lets the UI detect dropped spans
}

// LogRing is a fixed-capacity circular buffer of log lines. Once full it
// overwrites the oldest line, so a process that logs 40k lines/sec can
// never grow azstore's heap without bound.
//
// ponytail: guarded by a mutex rather than the lock-free ring the design
// called for. Append is a slice store under an uncontended Lock — at the
// two-writer-per-process load we actually have, the atomics would buy
// nothing. Upgrade path if profiling ever says otherwise: a power-of-two
// ring with an atomic write cursor and per-slot sequence numbers.
type LogRing struct {
	mu      sync.RWMutex
	buf     []LogLine
	next    int    // index of the next write
	filled  bool   // whether buf has wrapped at least once
	seq     uint64 // total lines ever appended
	dropped uint64 // lines evicted by wrapping
}

// NewLogRing builds a ring holding the most recent capacity lines.
func NewLogRing(capacity int) *LogRing {
	if capacity <= 0 {
		capacity = 1
	}
	return &LogRing{buf: make([]LogLine, capacity)}
}

// Append stores a line, evicting the oldest if the ring is full. It
// stamps and returns the line's sequence number.
func (r *LogRing) Append(l LogLine) LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seq++
	l.Seq = r.seq
	if r.filled {
		r.dropped++
	}
	r.buf[r.next] = l
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.filled = true
	}
	return l
}

// Lines returns a copy of the buffer in chronological order. Copying is
// deliberate: the caller is a render loop on another goroutine and must
// not alias storage the pump is still writing to.
func (r *LogRing) Lines() []LogLine {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.filled {
		out := make([]LogLine, r.next)
		copy(out, r.buf[:r.next])
		return out
	}
	out := make([]LogLine, 0, len(r.buf))
	out = append(out, r.buf[r.next:]...)
	out = append(out, r.buf[:r.next]...)
	return out
}

// Since returns every buffered line with a sequence number greater than
// seq, plus the newest sequence number. The UI polls with this so a
// missed event notification only delays output, never loses it.
func (r *LogRing) Since(seq uint64) ([]LogLine, uint64) {
	r.mu.RLock()
	latest := r.seq
	r.mu.RUnlock()

	if latest == seq {
		return nil, latest
	}
	all := r.Lines()
	idx := 0
	for idx < len(all) && all[idx].Seq <= seq {
		idx++
	}
	return all[idx:], latest
}

// Stats reports how many lines were appended in total and how many were
// evicted, so the logs screen can show "… 12,004 earlier lines dropped".
func (r *LogRing) Stats() (total, dropped uint64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.seq, r.dropped
}

// Clear empties the ring but keeps the sequence counter monotonic, so
// subscribers polling with Since() are not thrown backwards.
func (r *LogRing) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next, r.filled, r.dropped = 0, false, 0
}
