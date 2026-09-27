// Package storage is a thin facade over the Azure SDK. Every explorer
// screen talks to Azurite the same way any other client does — REST over
// HTTP to 127.0.0.1 — rather than reading Azurite's on-disk LokiJS files.
package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"

	"github.com/Linux-DEX/azstorecli/internal/config"
)

// ErrReadOnly is returned by Guard when a mutating call is attempted
// against a profile marked read-only. This is the guardrail that stops
// someone muscle-memory-deleting a production container.
var ErrReadOnly = errors.New("profile is read-only")

// ErrNoEndpoint means the profile does not configure the service the
// caller asked for.
var ErrNoEndpoint = errors.New("profile has no endpoint for this service")

// Clients lazily builds and caches the three service clients for one
// profile. Construction is deferred because a profile may only configure
// blob, and building a table client for it would fail for no reason.
type Clients struct {
	profile config.Profile

	mu          sync.Mutex
	blob        *azblob.Client
	queue       *azqueue.ServiceClient
	table       *aztables.ServiceClient
	unlockUntil time.Time
}

// New builds a client set for a profile.
func New(p config.Profile) *Clients { return &Clients{profile: p} }

// Profile returns the profile these clients target.
func (c *Clients) Profile() config.Profile { return c.profile }

// Guard reports whether a mutating operation is permitted. Callers must
// invoke it before every write; the read paths deliberately skip it.
func (c *Clients) Guard() error {
	if !c.profile.IsReadOnly() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.unlockUntil) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrReadOnly, c.profile.Name)
}

// Unlock temporarily suspends the read-only guard.
func (c *Clients) Unlock(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unlockUntil = time.Now().Add(d)
}

// UnlockedFor reports the remaining unlock window, for the status bar.
func (c *Clients) UnlockedFor() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := time.Until(c.unlockUntil); d > 0 {
		return d
	}
	return 0
}

// clientOptions caps retries. The SDK default of three retries with
// backoff turns a stopped Azurite into a 30-second UI freeze; against
// loopback, one retry is plenty and failing fast is the better report.
func clientOptions() azcore.ClientOptions {
	return azcore.ClientOptions{
		Retry: policy.RetryOptions{
			MaxRetries:    1,
			TryTimeout:    30 * time.Second,
			RetryDelay:    200 * time.Millisecond,
			MaxRetryDelay: 2 * time.Second,
		},
		Transport: &http.Client{Timeout: 60 * time.Second},
	}
}

// Blob returns the blob service client, building it on first use.
func (c *Clients) Blob() (*azblob.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blob != nil {
		return c.blob, nil
	}

	opts := &azblob.ClientOptions{ClientOptions: clientOptions()}
	var (
		client *azblob.Client
		err    error
	)
	switch {
	case c.profile.Auth == config.AuthEntra:
		if c.profile.BlobEndpoint == "" {
			return nil, ErrNoEndpoint
		}
		cred, credErr := azidentity.NewDefaultAzureCredential(nil)
		if credErr != nil {
			return nil, fmt.Errorf("entra credential: %w", credErr)
		}
		client, err = azblob.NewClient(c.profile.BlobEndpoint, cred, opts)
	case c.profile.Auth == config.AuthSAS:
		if c.profile.BlobEndpoint == "" {
			return nil, ErrNoEndpoint
		}
		client, err = azblob.NewClientWithNoCredential(withSAS(c.profile.BlobEndpoint, c.profile.SASToken), opts)
	default:
		// NewClientFromConnectionString handles Azurite's path-style URL
		// (account name in the path, not the subdomain) on its own.
		cs := c.profile.ConnString()
		if cs == "" {
			return nil, ErrNoEndpoint
		}
		client, err = azblob.NewClientFromConnectionString(cs, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("blob client: %w", err)
	}
	c.blob = client
	return client, nil
}

// Queue returns the queue service client.
func (c *Clients) Queue() (*azqueue.ServiceClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.queue != nil {
		return c.queue, nil
	}

	opts := &azqueue.ClientOptions{ClientOptions: clientOptions()}
	var (
		client *azqueue.ServiceClient
		err    error
	)
	switch {
	case c.profile.Auth == config.AuthEntra:
		if c.profile.QueueEndpoint == "" {
			return nil, ErrNoEndpoint
		}
		cred, credErr := azidentity.NewDefaultAzureCredential(nil)
		if credErr != nil {
			return nil, fmt.Errorf("entra credential: %w", credErr)
		}
		client, err = azqueue.NewServiceClient(c.profile.QueueEndpoint, cred, opts)
	case c.profile.Auth == config.AuthSAS:
		if c.profile.QueueEndpoint == "" {
			return nil, ErrNoEndpoint
		}
		client, err = azqueue.NewServiceClientWithNoCredential(withSAS(c.profile.QueueEndpoint, c.profile.SASToken), opts)
	default:
		cs := c.profile.ConnString()
		if cs == "" {
			return nil, ErrNoEndpoint
		}
		client, err = azqueue.NewServiceClientFromConnectionString(cs, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("queue client: %w", err)
	}
	c.queue = client
	return client, nil
}

// Table returns the table service client.
func (c *Clients) Table() (*aztables.ServiceClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.table != nil {
		return c.table, nil
	}

	opts := &aztables.ClientOptions{ClientOptions: clientOptions()}
	var (
		client *aztables.ServiceClient
		err    error
	)
	switch {
	case c.profile.Auth == config.AuthEntra:
		if c.profile.TableEndpoint == "" {
			return nil, ErrNoEndpoint
		}
		cred, credErr := azidentity.NewDefaultAzureCredential(nil)
		if credErr != nil {
			return nil, fmt.Errorf("entra credential: %w", credErr)
		}
		client, err = aztables.NewServiceClient(c.profile.TableEndpoint, cred, opts)
	case c.profile.Auth == config.AuthSAS:
		if c.profile.TableEndpoint == "" {
			return nil, ErrNoEndpoint
		}
		client, err = aztables.NewServiceClientWithNoCredential(withSAS(c.profile.TableEndpoint, c.profile.SASToken), opts)
	default:
		cs := c.profile.ConnString()
		if cs == "" {
			return nil, ErrNoEndpoint
		}
		client, err = aztables.NewServiceClientFromConnectionString(cs, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("table client: %w", err)
	}
	c.table = client
	return client, nil
}

// Test verifies the profile can reach every configured service, and
// reports which one failed rather than a single opaque error.
func (c *Clients) Test(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var errs []error
	if c.profile.BlobEndpoint != "" || c.profile.ConnectionString != "" {
		if client, err := c.Blob(); err != nil {
			errs = append(errs, fmt.Errorf("blob: %w", err))
		} else {
			pager := client.NewListContainersPager(nil)
			if _, err := pager.NextPage(ctx); err != nil {
				errs = append(errs, fmt.Errorf("blob: %w", err))
			}
		}
	}
	if c.profile.QueueEndpoint != "" || c.profile.ConnectionString != "" {
		if client, err := c.Queue(); err != nil {
			errs = append(errs, fmt.Errorf("queue: %w", err))
		} else {
			pager := client.NewListQueuesPager(nil)
			if _, err := pager.NextPage(ctx); err != nil {
				errs = append(errs, fmt.Errorf("queue: %w", err))
			}
		}
	}
	if c.profile.TableEndpoint != "" || c.profile.ConnectionString != "" {
		if client, err := c.Table(); err != nil {
			errs = append(errs, fmt.Errorf("table: %w", err))
		} else {
			pager := client.NewListTablesPager(nil)
			if _, err := pager.NextPage(ctx); err != nil {
				errs = append(errs, fmt.Errorf("table: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}

// withSAS appends a SAS token to an endpoint, tolerating either
// spelling of the leading separator.
func withSAS(endpoint, token string) string {
	if token == "" {
		return endpoint
	}
	token = strings.TrimPrefix(token, "?")
	if strings.Contains(endpoint, "?") {
		return endpoint + "&" + token
	}
	return endpoint + "?" + token
}

// deref safely reads a *T the SDK may leave nil.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// ptr is the inverse, for the many *string fields in SDK option structs.
func ptr[T any](v T) *T { return &v }
