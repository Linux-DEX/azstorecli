package supervisor

import (
	"fmt"
	"net"
	"strconv"

	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// PortFree reports whether a TCP port on the loopback interface can be
// bound right now.
//
// We bind 127.0.0.1 specifically, not the wildcard: Azurite binds
// loopback by default, so a wildcard probe would report "in use" for a
// port that is genuinely available to us, and would miss the case where
// something else already holds loopback but not the wildcard.
func PortFree(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// PortHolder identifies the process listening on a port, so a conflict
// can be reported as "port 10000 held by node (pid 48213)" instead of a
// bare bind error. Returns ok=false when nothing holds it or when the OS
// withholds ownership info (macOS needs elevation for other users'
// sockets) — the caller should degrade to the generic message.
func PortHolder(port int) (pid int32, name string, ok bool) {
	conns, err := psnet.Connections("inet")
	if err != nil {
		return 0, "", false
	}
	for _, c := range conns {
		if c.Status != "LISTEN" || int(c.Laddr.Port) != port || c.Pid == 0 {
			continue
		}
		p, err := process.NewProcess(c.Pid)
		if err != nil {
			return c.Pid, "", true
		}
		n, err := p.Name()
		if err != nil {
			return c.Pid, "", true
		}
		return c.Pid, n, true
	}
	return 0, "", false
}

// PortConflict describes one unavailable port.
type PortConflict struct {
	Service string
	Port    int
	PID     int32
	Holder  string
}

func (c PortConflict) Error() string {
	if c.Holder != "" {
		return fmt.Sprintf("%s port %d is held by %s (pid %d)", c.Service, c.Port, c.Holder, c.PID)
	}
	return fmt.Sprintf("%s port %d is already in use", c.Service, c.Port)
}

// CheckPorts validates a service:port map before a start attempt, so the
// user sees which process to kill rather than a runtime bind error
// buried in child stderr.
func CheckPorts(ports map[string]int) []PortConflict {
	var conflicts []PortConflict
	for svc, port := range ports {
		if port <= 0 || port > 65535 {
			conflicts = append(conflicts, PortConflict{Service: svc, Port: port})
			continue
		}
		if PortFree(port) {
			continue
		}
		pid, name, _ := PortHolder(port)
		conflicts = append(conflicts, PortConflict{Service: svc, Port: port, PID: pid, Holder: name})
	}
	return conflicts
}

// FreePortNear returns the first free port at or after start, searching
// up to span candidates. Used by the onboarding wizard to suggest
// alternatives when the default trio is taken.
func FreePortNear(start, span int) (int, bool) {
	for p := start; p < start+span && p <= 65535; p++ {
		if PortFree(p) {
			return p, true
		}
	}
	return 0, false
}
