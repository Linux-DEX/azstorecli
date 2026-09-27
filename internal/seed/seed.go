// Package seed applies declarative fixture data through the Azure SDK.
//
// Snapshots are binary and version-locked to the Azurite build that made
// them, which makes them a poor thing to commit. A seed file is text,
// diffable, and reproducible on any Azurite version — this is what
// belongs in git and in CI.
package seed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Linux-DEX/azstorecli/internal/storage"
	"github.com/Linux-DEX/azstorecli/internal/util"
)

// Doc is a seed file.
type Doc struct {
	Version int `yaml:"version"`

	Blob struct {
		Containers []Container `yaml:"containers"`
	} `yaml:"blob"`

	Queue struct {
		Queues []Queue `yaml:"queues"`
	} `yaml:"queue"`

	Table struct {
		Tables []Table `yaml:"tables"`
	} `yaml:"table"`

	// dir is the seed file's directory; every `file:` reference
	// resolves against it, never against the process working directory.
	dir string
}

// Container is one blob container and its contents.
type Container struct {
	Name   string `yaml:"name"`
	Access string `yaml:"access"` // private | blob | container
	Blobs  []Blob `yaml:"blobs"`
}

// Blob is one seeded blob. Exactly one of File or Content must be set.
type Blob struct {
	Name        string            `yaml:"name"`
	File        string            `yaml:"file"`
	Content     string            `yaml:"content"`
	ContentType string            `yaml:"contentType"`
	Metadata    map[string]string `yaml:"metadata"`
}

// Queue is one queue and its messages.
type Queue struct {
	Name     string        `yaml:"name"`
	Messages []QueueMsg    `yaml:"messages"`
	TTL      time.Duration `yaml:"ttl"`
}

// QueueMsg is one seeded message.
type QueueMsg struct {
	File    string `yaml:"file"`
	Body    string `yaml:"body"`
	Base64  bool   `yaml:"base64"`
	Visible string `yaml:"visibleIn"` // e.g. "30s"
}

// Table is one table and its entities.
type Table struct {
	Name     string           `yaml:"name"`
	Entities []map[string]any `yaml:"entities"`
}

// Result reports what an apply did, for the CLI summary.
type Result struct {
	ContainersCreated int
	BlobsWritten      int
	BlobsSkipped      int
	QueuesCreated     int
	MessagesEnqueued  int
	TablesCreated     int
	EntitiesUpserted  int
}

func (r Result) String() string {
	return fmt.Sprintf(
		"%d containers, %d blobs (%d unchanged), %d queues, %d messages, %d tables, %d entities",
		r.ContainersCreated, r.BlobsWritten, r.BlobsSkipped,
		r.QueuesCreated, r.MessagesEnqueued, r.TablesCreated, r.EntitiesUpserted)
}

// Load reads and validates a seed file.
func Load(path string) (*Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc Doc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	doc.dir = filepath.Dir(abs)
	return &doc, doc.Validate()
}

// Validate catches the mistakes that would otherwise surface as a
// partial apply — half the fixtures loaded, then a failure.
func (d *Doc) Validate() error {
	var errs []error

	if d.Version != 0 && d.Version != 1 {
		errs = append(errs, fmt.Errorf("unsupported seed version %d", d.Version))
	}

	for _, c := range d.Blob.Containers {
		if c.Name == "" {
			errs = append(errs, errors.New("a blob container has no name"))
		}
		switch c.Access {
		case "", "private", "blob", "container":
		default:
			errs = append(errs, fmt.Errorf("container %s: access %q must be private, blob, or container", c.Name, c.Access))
		}
		for _, b := range c.Blobs {
			if b.Name == "" {
				errs = append(errs, fmt.Errorf("container %s: a blob has no name", c.Name))
			}
			if (b.File == "") == (b.Content == "") {
				errs = append(errs, fmt.Errorf("blob %s: set exactly one of `file` or `content`", b.Name))
			}
			if b.File != "" {
				if _, err := d.resolve(b.File); err != nil {
					errs = append(errs, fmt.Errorf("blob %s: %w", b.Name, err))
				}
			}
		}
	}

	for _, q := range d.Queue.Queues {
		if q.Name == "" {
			errs = append(errs, errors.New("a queue has no name"))
		}
		for i, m := range q.Messages {
			if (m.File == "") == (m.Body == "") {
				errs = append(errs, fmt.Errorf("queue %s message %d: set exactly one of `file` or `body`", q.Name, i+1))
			}
			if m.Visible != "" {
				if _, err := time.ParseDuration(m.Visible); err != nil {
					errs = append(errs, fmt.Errorf("queue %s message %d: visibleIn %q is not a duration", q.Name, i+1, m.Visible))
				}
			}
		}
	}

	for _, t := range d.Table.Tables {
		if t.Name == "" {
			errs = append(errs, errors.New("a table has no name"))
		}
		for i, e := range t.Entities {
			if _, ok := e["PartitionKey"]; !ok {
				errs = append(errs, fmt.Errorf("table %s entity %d: missing PartitionKey", t.Name, i+1))
			}
			if _, ok := e["RowKey"]; !ok {
				errs = append(errs, fmt.Errorf("table %s entity %d: missing RowKey", t.Name, i+1))
			}
		}
	}

	return errors.Join(errs...)
}

// resolve turns a fixture reference into an absolute path, refusing any
// that escapes the seed directory. A seed file is committed and shared;
// it must not be able to read ~/.ssh/id_rsa and upload it.
func (d *Doc) resolve(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("fixture path %q must be relative to the seed file", rel)
	}
	abs, err := util.SafeJoin(d.dir, rel)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("fixture %s: %w", rel, err)
	}
	return abs, nil
}

// Apply writes the seed to storage. It is idempotent: containers,
// queues, and tables are create-if-not-exists, entities are upserted,
// and a blob is skipped when its content already matches — so a
// re-apply in CI is a no-op rather than a churn of new versions.
func (d *Doc) Apply(ctx context.Context, c *storage.Clients) (Result, error) {
	if err := c.Guard(); err != nil {
		return Result{}, fmt.Errorf("refusing to seed: %w", err)
	}

	var res Result
	var errs []error

	for _, container := range d.Blob.Containers {
		if err := c.CreateContainer(ctx, container.Name, container.Access); err != nil {
			errs = append(errs, err)
			continue
		}
		res.ContainersCreated++

		for _, b := range container.Blobs {
			data, err := d.blobBytes(b)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if unchanged(ctx, c, container.Name, b.Name, data) {
				res.BlobsSkipped++
				continue
			}

			contentType := b.ContentType
			if contentType == "" {
				contentType = storage.GuessContentType(b.Name)
			}
			if err := c.WriteAll(ctx, container.Name, b.Name, data, contentType); err != nil {
				errs = append(errs, err)
				continue
			}
			if len(b.Metadata) > 0 {
				if err := c.SetMetadata(ctx, container.Name, b.Name, b.Metadata); err != nil {
					errs = append(errs, err)
				}
			}
			res.BlobsWritten++
		}
	}

	for _, q := range d.Queue.Queues {
		if err := c.CreateQueue(ctx, q.Name); err != nil {
			errs = append(errs, err)
			continue
		}
		res.QueuesCreated++

		for i, m := range q.Messages {
			body, err := d.messageBody(m)
			if err != nil {
				errs = append(errs, fmt.Errorf("queue %s message %d: %w", q.Name, i+1, err))
				continue
			}
			var delay time.Duration
			if m.Visible != "" {
				delay, _ = time.ParseDuration(m.Visible)
			}
			if _, err := c.EnqueueMessage(ctx, q.Name, body, m.Base64, q.TTL, delay); err != nil {
				errs = append(errs, err)
				continue
			}
			res.MessagesEnqueued++
		}
	}

	for _, t := range d.Table.Tables {
		if err := c.CreateTable(ctx, t.Name); err != nil {
			errs = append(errs, err)
			continue
		}
		res.TablesCreated++

		for i, e := range t.Entities {
			if err := c.UpsertEntity(ctx, t.Name, normaliseKeys(e)); err != nil {
				errs = append(errs, fmt.Errorf("table %s entity %d: %w", t.Name, i+1, err))
				continue
			}
			res.EntitiesUpserted++
		}
	}

	return res, errors.Join(errs...)
}

func (d *Doc) blobBytes(b Blob) ([]byte, error) {
	if b.Content != "" {
		return []byte(b.Content), nil
	}
	path, err := d.resolve(b.File)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (d *Doc) messageBody(m QueueMsg) (string, error) {
	if m.Body != "" {
		return m.Body, nil
	}
	path, err := d.resolve(m.File)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// unchanged reports whether the blob already holds exactly this content,
// so a re-apply does not rewrite it. Only small blobs are compared:
// pulling a 500 MB fixture back down to avoid one upload is a bad trade.
func unchanged(ctx context.Context, c *storage.Clients, container, name string, want []byte) bool {
	const compareLimit = 1 << 20
	if len(want) > compareLimit {
		return false
	}
	props, err := c.Properties(ctx, container, name)
	if err != nil || props.Size != int64(len(want)) {
		return false
	}
	got, truncated, err := c.ReadRange(ctx, container, name, compareLimit)
	if err != nil || truncated {
		return false
	}
	return string(got) == string(want)
}

// normaliseKeys converts a YAML-decoded entity into the shape aztables
// expects. YAML gives ints as int and maps as map[string]any; the table
// wire format needs JSON-representable scalars with string keys.
func normaliseKeys(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normaliseValue(v)
	}
	return out
}

func normaliseValue(v any) any {
	switch t := v.(type) {
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = normaliseValue(val)
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = normaliseValue(item)
		}
		return out
	case int:
		return int64(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	default:
		return v
	}
}
