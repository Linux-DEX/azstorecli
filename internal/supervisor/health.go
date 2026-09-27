package supervisor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// HealthCheck decides whether a process is actually serving, as opposed
// to merely having a live PID.
type HealthCheck interface {
	// Probe returns nil when the service is answering correctly.
	Probe(ctx context.Context) error
	// Policy reports how often to probe and how long to keep trying
	// during startup before declaring the process unhealthy.
	Policy() ProbePolicy
	// Describe renders the probe target for the dashboard detail view.
	Describe() string
}

// ProbePolicy is the timing envelope around a HealthCheck.
type ProbePolicy struct {
	Interval time.Duration
	Timeout  time.Duration
	Retries  int
}

func (p ProbePolicy) withDefaults() ProbePolicy {
	if p.Interval <= 0 {
		p.Interval = 2 * time.Second
	}
	if p.Timeout <= 0 {
		p.Timeout = time.Second
	}
	if p.Retries <= 0 {
		p.Retries = 15
	}
	return p
}

// HTTPCheck probes an HTTP endpoint and accepts whatever status codes
// Accept approves. Azurite never returns 200 to an unauthenticated
// request, so "did not 5xx and did not refuse the connection" is the
// real signal, not "returned 200".
type HTTPCheck struct {
	URL      string
	Accept   func(code int) bool
	Interval time.Duration
	Timeout  time.Duration
	Retries  int

	client *http.Client
}

func (h HTTPCheck) Policy() ProbePolicy {
	return ProbePolicy{Interval: h.Interval, Timeout: h.Timeout, Retries: h.Retries}.withDefaults()
}

func (h HTTPCheck) Describe() string { return "GET " + h.URL }

func (h HTTPCheck) Probe(ctx context.Context) error {
	p := h.Policy()
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return err
	}

	c := h.client
	if c == nil {
		c = healthClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	// Drain before closing so the connection returns to the pool rather
	// than being torn down on every 2s probe.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()

	accept := h.Accept
	if accept == nil {
		accept = func(code int) bool { return code >= 200 && code < 400 }
	}
	if !accept(resp.StatusCode) {
		return fmt.Errorf("health: %s returned %d", h.URL, resp.StatusCode)
	}
	return nil
}

// healthClient is shared by every probe. Probes are frequent and tiny,
// so connection reuse matters more than isolation here.
var healthClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 4,
		DisableCompression:  true,
	},
}

// TCPCheck is the fallback probe for a service with no HTTP surface: it
// only proves something is accepting connections on the port.
type TCPCheck struct {
	Addr     string
	Interval time.Duration
	Timeout  time.Duration
	Retries  int
}

func (t TCPCheck) Policy() ProbePolicy {
	return ProbePolicy{Interval: t.Interval, Timeout: t.Timeout, Retries: t.Retries}.withDefaults()
}

func (t TCPCheck) Describe() string { return "dial " + t.Addr }

func (t TCPCheck) Probe(ctx context.Context) error {
	p := t.Policy()
	d := net.Dialer{Timeout: p.Timeout}
	conn, err := d.DialContext(ctx, "tcp", t.Addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// WaitHealthy probes until the check passes or the retry budget runs
// out. Backoff is capped at the configured interval so a slow first
// start does not stretch into minutes.
func WaitHealthy(ctx context.Context, hc HealthCheck) error {
	p := hc.Policy()
	var last error
	for attempt := 0; attempt < p.Retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if last = hc.Probe(ctx); last == nil {
			return nil
		}
		if err := util.Sleep(ctx, util.Backoff(attempt, 200*time.Millisecond, p.Interval)); err != nil {
			return err
		}
	}
	return fmt.Errorf("never became healthy after %d probes: %w", p.Retries, last)
}

// AzuriteHealth probes Azurite's blob endpoint. An unauthenticated
// ListContainers gets a 400 InvalidQueryParameterValue back — which is
// itself proof the service is up and parsing requests, so anything
// below 500 counts as healthy.
func AzuriteHealth(port int) HealthCheck {
	return HTTPCheck{
		URL:      fmt.Sprintf("http://127.0.0.1:%d/devstoreaccount1?comp=list", port),
		Accept:   func(code int) bool { return code > 0 && code < 500 },
		Interval: 2 * time.Second,
		Timeout:  time.Second,
		Retries:  15,
	}
}

// FunctionsHealth probes the Functions host admin endpoint. The host
// binds its port well before the worker finishes loading, so a TCP dial
// would report healthy too early; /admin/host/status only answers once
// the runtime is actually up.
func FunctionsHealth(port int) HealthCheck {
	return HTTPCheck{
		URL:      fmt.Sprintf("http://127.0.0.1:%d/admin/host/status", port),
		Accept:   func(code int) bool { return code > 0 && code < 500 },
		Interval: 2 * time.Second,
		Timeout:  2 * time.Second,
		Retries:  45, // cold `func host start` routinely takes 30s+
	}
}
