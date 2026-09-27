package funcs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client invokes functions on a running host.
type Client struct {
	baseURL string
	key     string // master key for /admin endpoints, when the host needs one
	http    *http.Client
}

// NewClient builds an invoker for a host base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		// Generous but finite: a cold first invoke can take 30s while
		// the language worker JITs, but a hang must not wedge the TUI.
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

// WithKey attaches a function key, needed when authLevel is not
// anonymous.
func (c *Client) WithKey(key string) *Client { c.key = key; return c }

// Request is one invocation.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Query   map[string]string
	Body    string
}

// Response is the result of an invocation.
type Response struct {
	Status   int
	Headers  http.Header
	Body     string
	Duration time.Duration
	Err      error
}

// InvokeHTTP calls an HTTP-triggered function.
func (c *Client) InvokeHTTP(ctx context.Context, req Request) Response {
	started := time.Now()

	target := req.URL
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = c.baseURL + "/" + strings.TrimPrefix(target, "/")
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return Response{Err: err, Duration: time.Since(started)}
	}
	q := parsed.Query()
	for k, v := range req.Query {
		q.Set(k, v)
	}
	if c.key != "" && q.Get("code") == "" {
		q.Set("code", c.key)
	}
	parsed.RawQuery = q.Encode()

	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return Response{Err: err, Duration: time.Since(started)}
	}
	// Default to JSON only when the caller sent a body and named no
	// type; sending Content-Type on a GET confuses some middlewares.
	if req.Body != "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Response{Err: err, Duration: time.Since(started)}
	}
	defer resp.Body.Close()

	// Cap the response: a function returning a 200 MB blob must not be
	// loaded whole into a viewport.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return Response{
		Status:   resp.StatusCode,
		Headers:  resp.Header,
		Body:     string(data),
		Duration: time.Since(started),
		Err:      err,
	}
}

// InvokeNonHTTP triggers a queue-, timer-, or blob-triggered function
// through the admin endpoint.
//
// The host expects {"input": "<serialised trigger payload>"} — note the
// payload is a *string*, even for a JSON body. Passing a nested object
// gets a 400 that says nothing useful.
func (c *Client) InvokeNonHTTP(ctx context.Context, name string, input string) Response {
	started := time.Now()

	payload, err := json.Marshal(map[string]string{"input": input})
	if err != nil {
		return Response{Err: err, Duration: time.Since(started)}
	}

	target := fmt.Sprintf("%s/admin/functions/%s", c.baseURL, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return Response{Err: err, Duration: time.Since(started)}
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("x-functions-key", c.key)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Response{Err: err, Duration: time.Since(started)}
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	out := Response{
		Status:   resp.StatusCode,
		Headers:  resp.Header,
		Body:     string(data),
		Duration: time.Since(started),
	}
	// The admin endpoint answers 202 and returns nothing; the actual
	// result shows up in the host log, so say so rather than leaving an
	// empty response pane that looks like a failure.
	if resp.StatusCode == http.StatusAccepted && len(data) == 0 {
		out.Body = "202 Accepted — the trigger fired; watch the Logs screen for its output."
	}
	return out
}

// HostStatus is the /admin/host/status document.
type HostStatus struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Version string `json:"version"`
}

// Status queries the host's own health report.
func (c *Client) Status(ctx context.Context) (HostStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/admin/host/status", nil)
	if err != nil {
		return HostStatus{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return HostStatus{}, err
	}
	defer resp.Body.Close()

	var s HostStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&s); err != nil {
		return HostStatus{}, err
	}
	return s, nil
}

// CurlCommand renders an invocation as a copyable curl one-liner.
func CurlCommand(req Request) string {
	var b strings.Builder
	b.WriteString("curl -i -X " + orDefault(req.Method, "GET"))
	for k, v := range req.Headers {
		fmt.Fprintf(&b, " \\\n  -H %s", shellQuote(k+": "+v))
	}
	if req.Body != "" {
		if _, ok := req.Headers["Content-Type"]; !ok {
			b.WriteString(" \\\n  -H 'Content-Type: application/json'")
		}
		fmt.Fprintf(&b, " \\\n  -d %s", shellQuote(req.Body))
	}
	fmt.Fprintf(&b, " \\\n  %s", shellQuote(req.URL))
	return b.String()
}

// shellQuote wraps a value in single quotes, escaping any it contains.
// Without this, a JSON body with an apostrophe produces a curl command
// that silently truncates.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
