package storage

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
)

// PoisonThreshold is the Functions host's default maxDequeueCount. A
// message approaching it is about to be moved to the poison queue, which
// is exactly when a developer wants to notice it.
const PoisonThreshold = 5

// QueueInfo summarises one queue for the sidebar.
type QueueInfo struct {
	Name           string
	ApproxMessages int32
	Metadata       map[string]string
}

// Message is one queue message, from either a peek or a dequeue.
type Message struct {
	ID              string
	PopReceipt      string
	Body            string
	Raw             string // the wire form, before any base64 decode
	Base64Encoded   bool
	InsertionTime   time.Time
	ExpirationTime  time.Time
	NextVisibleTime time.Time
	DequeueCount    int64
	SizeBytes       int
}

// NearPoison reports whether this message is about to be dead-lettered.
func (m Message) NearPoison() bool { return m.DequeueCount >= PoisonThreshold-2 }

// ListQueues returns every queue with its approximate message count.
func (c *Clients) ListQueues(ctx context.Context) ([]QueueInfo, error) {
	svc, err := c.Queue()
	if err != nil {
		return nil, err
	}

	var out []QueueInfo
	// Do not ask for metadata. Azurite's include=metadata listing omits
	// queues that have none, so a queue created earlier never comes back.
	pager := svc.NewListQueuesPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, wrapAzure("list queues", err)
		}
		for _, q := range page.Queues {
			if q == nil || q.Name == nil {
				continue
			}
			info := QueueInfo{Name: *q.Name, Metadata: map[string]string{}}
			for k, v := range q.Metadata {
				info.Metadata[k] = deref(v)
			}
			out = append(out, info)
		}
	}

	// The list API does not carry message counts; one GetProperties per
	// queue does. Local queue counts are in the single digits, so the
	// extra round trips are cheaper than showing nothing.
	for i := range out {
		props, err := svc.NewQueueClient(out[i].Name).GetProperties(ctx, nil)
		if err != nil {
			continue
		}
		out[i].ApproxMessages = deref(props.ApproximateMessagesCount)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// PeekMessages reads messages without making them invisible, so the
// Functions host can still pick them up while you are looking.
func (c *Clients) PeekMessages(ctx context.Context, queue string, n int32) ([]Message, error) {
	svc, err := c.Queue()
	if err != nil {
		return nil, err
	}
	if n <= 0 || n > 32 {
		n = 32 // the service's hard ceiling
	}

	resp, err := svc.NewQueueClient(queue).PeekMessages(ctx, &azqueue.PeekMessagesOptions{
		NumberOfMessages: &n,
	})
	if err != nil {
		return nil, wrapAzure("peek "+queue, err)
	}

	out := make([]Message, 0, len(resp.Messages))
	for _, m := range resp.Messages {
		if m == nil {
			continue
		}
		out = append(out, Message{
			ID:             deref(m.MessageID),
			Body:           decodeBody(deref(m.MessageText)),
			Raw:            deref(m.MessageText),
			Base64Encoded:  isBase64(deref(m.MessageText)),
			InsertionTime:  deref(m.InsertionTime),
			ExpirationTime: deref(m.ExpirationTime),
			DequeueCount:   deref(m.DequeueCount),
			SizeBytes:      len(deref(m.MessageText)),
		})
	}
	return out, nil
}

// DequeueMessages takes messages off the queue for visibilityTimeout.
// This is destructive from the Functions host's point of view — it
// cannot see a message you are holding — so the UI defaults to peek.
func (c *Clients) DequeueMessages(ctx context.Context, queue string, n int32, visibility time.Duration) ([]Message, error) {
	if err := c.Guard(); err != nil {
		return nil, err
	}
	svc, err := c.Queue()
	if err != nil {
		return nil, err
	}
	if n <= 0 || n > 32 {
		n = 32
	}
	secs := int32(visibility.Seconds())
	if secs <= 0 {
		secs = 30
	}

	resp, err := svc.NewQueueClient(queue).DequeueMessages(ctx, &azqueue.DequeueMessagesOptions{
		NumberOfMessages:  &n,
		VisibilityTimeout: &secs,
	})
	if err != nil {
		return nil, wrapAzure("dequeue from "+queue, err)
	}

	out := make([]Message, 0, len(resp.Messages))
	for _, m := range resp.Messages {
		if m == nil {
			continue
		}
		out = append(out, Message{
			ID:              deref(m.MessageID),
			PopReceipt:      deref(m.PopReceipt),
			Body:            decodeBody(deref(m.MessageText)),
			Raw:             deref(m.MessageText),
			Base64Encoded:   isBase64(deref(m.MessageText)),
			InsertionTime:   deref(m.InsertionTime),
			ExpirationTime:  deref(m.ExpirationTime),
			NextVisibleTime: deref(m.TimeNextVisible),
			DequeueCount:    deref(m.DequeueCount),
			SizeBytes:       len(deref(m.MessageText)),
		})
	}
	return out, nil
}

// EnqueueMessage adds a message.
//
// encodeBase64 must match the binding's configuration: the Node and
// Python Functions workers base64-encode queue payloads by default,
// so a plain-text message they wrote round-trips only if we encode too.
func (c *Clients) EnqueueMessage(ctx context.Context, queue, body string, encodeBase64 bool, ttl, delay time.Duration) (string, error) {
	if err := c.Guard(); err != nil {
		return "", err
	}
	svc, err := c.Queue()
	if err != nil {
		return "", err
	}

	content := body
	if encodeBase64 {
		content = base64.StdEncoding.EncodeToString([]byte(body))
	}
	// 64 KiB is the service limit, and it applies to the encoded form.
	if len(content) > 64*1024 {
		return "", fmt.Errorf("message is %d bytes; the queue limit is 64 KiB", len(content))
	}

	opts := &azqueue.EnqueueMessageOptions{}
	if ttl > 0 {
		opts.TimeToLive = to.Ptr(int32(ttl.Seconds()))
	}
	if delay > 0 {
		opts.VisibilityTimeout = to.Ptr(int32(delay.Seconds()))
	}

	resp, err := svc.NewQueueClient(queue).EnqueueMessage(ctx, content, opts)
	if err != nil {
		return "", wrapAzure("enqueue to "+queue, err)
	}
	if len(resp.Messages) > 0 && resp.Messages[0] != nil {
		return deref(resp.Messages[0].MessageID), nil
	}
	return "", nil
}

// DeleteMessage removes a message. It needs a pop receipt, which only a
// dequeue returns — so a delete driven from the peek list dequeues the
// single message first.
func (c *Clients) DeleteMessage(ctx context.Context, queue, messageID, popReceipt string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Queue()
	if err != nil {
		return err
	}
	_, err = svc.NewQueueClient(queue).DeleteMessage(ctx, messageID, popReceipt, nil)
	return wrapAzure("delete message", err)
}

// DeleteMessageByID finds a message by ID, claims it, and deletes it.
// Peeked messages carry no pop receipt, so this is the only way to
// delete the row the user selected in the list.
func (c *Clients) DeleteMessageByID(ctx context.Context, queue, messageID string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	// A short visibility timeout limits the damage if the delete fails:
	// the message reappears in 10s rather than being invisible for 30.
	claimed, err := c.DequeueMessages(ctx, queue, 32, 10*time.Second)
	if err != nil {
		return err
	}
	for _, m := range claimed {
		if m.ID == messageID {
			return c.DeleteMessage(ctx, queue, m.ID, m.PopReceipt)
		}
	}
	return fmt.Errorf("message %s is not currently visible; it may already be claimed", short(messageID))
}

// UpdateVisibility changes how long a claimed message stays hidden.
func (c *Clients) UpdateVisibility(ctx context.Context, queue, messageID, popReceipt string, d time.Duration) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Queue()
	if err != nil {
		return err
	}
	_, err = svc.NewQueueClient(queue).UpdateMessage(ctx, messageID, popReceipt, "", &azqueue.UpdateMessageOptions{
		VisibilityTimeout: to.Ptr(int32(d.Seconds())),
	})
	return wrapAzure("update visibility", err)
}

// ClearQueue deletes every message in a queue.
func (c *Clients) ClearQueue(ctx context.Context, queue string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Queue()
	if err != nil {
		return err
	}
	_, err = svc.NewQueueClient(queue).ClearMessages(ctx, nil)
	return wrapAzure("clear "+queue, err)
}

// CreateQueue creates a queue idempotently.
func (c *Clients) CreateQueue(ctx context.Context, name string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Queue()
	if err != nil {
		return err
	}
	_, err = svc.CreateQueue(ctx, name, nil)
	if isAlreadyExists(err) {
		return nil
	}
	return wrapAzure("create queue "+name, err)
}

// DeleteQueue removes a queue and its messages.
func (c *Clients) DeleteQueue(ctx context.Context, name string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Queue()
	if err != nil {
		return err
	}
	_, err = svc.DeleteQueue(ctx, name, nil)
	return wrapAzure("delete queue "+name, err)
}

// Requeue moves a message from one queue to another — the dead-letter
// to main-queue replay. The source copy is deleted only after the
// destination write succeeds, so a failure never loses the message.
func (c *Clients) Requeue(ctx context.Context, srcQueue, dstQueue, messageID string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	if srcQueue == dstQueue {
		return errors.New("source and destination queues are the same")
	}
	claimed, err := c.DequeueMessages(ctx, srcQueue, 32, 30*time.Second)
	if err != nil {
		return err
	}
	for _, m := range claimed {
		if m.ID != messageID {
			continue
		}
		// Re-send the wire form verbatim: decoding and re-encoding would
		// corrupt a payload that was never base64 to begin with.
		if _, err := c.enqueueRaw(ctx, dstQueue, m.Raw); err != nil {
			return err
		}
		return c.DeleteMessage(ctx, srcQueue, m.ID, m.PopReceipt)
	}
	return fmt.Errorf("message %s is not currently visible", short(messageID))
}

func (c *Clients) enqueueRaw(ctx context.Context, queue, wire string) (string, error) {
	svc, err := c.Queue()
	if err != nil {
		return "", err
	}
	resp, err := svc.NewQueueClient(queue).EnqueueMessage(ctx, wire, nil)
	if err != nil {
		return "", wrapAzure("enqueue to "+queue, err)
	}
	if len(resp.Messages) > 0 && resp.Messages[0] != nil {
		return deref(resp.Messages[0].MessageID), nil
	}
	return "", nil
}

// decodeBody renders a message for display, decoding base64 when the
// payload plausibly is base64.
func decodeBody(raw string) string {
	if !isBase64(raw) {
		return raw
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return raw
	}
	return string(decoded)
}

// isBase64 guesses whether a queue payload is base64-encoded.
//
// There is no flag on the wire saying so, and the Node worker encodes
// while a `az storage message put` does not. The heuristic: it must
// decode cleanly, and the decoded bytes must look like text. Requiring
// printable output is what stops a plain JSON body — which is often
// valid base64 by accident — from being mangled into binary noise.
func isBase64(s string) bool {
	if len(s) < 4 || len(s)%4 != 0 || strings.ContainsAny(s, "{}<>\n\r ") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return false
	}
	return isPrintable(decoded)
}

func isPrintable(b []byte) bool {
	for _, r := range string(b) {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0xFFFD {
			return false
		}
	}
	return len(b) > 0
}

// short truncates an ID for an error message.
func short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "…"
}
