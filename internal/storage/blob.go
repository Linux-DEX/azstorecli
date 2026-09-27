package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/lease"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"

	"github.com/Linux-DEX/azstorecli/internal/config"
)

// Delimiter is the virtual-directory separator used for hierarchical
// listing. Azure has no real directories; a prefix ending in "/" is the
// closest thing.
const Delimiter = "/"

// ContainerInfo summarises one container for the sidebar.
type ContainerInfo struct {
	Name         string
	LastModified time.Time
	Access       string
	LeaseState   string
}

// BlobEntry is one row in the blob list: either a virtual directory or
// a blob.
type BlobEntry struct {
	Name         string // full blob name, or prefix for a directory
	Display      string // leaf segment shown in the table
	IsDir        bool
	Size         int64
	BlobType     string
	ContentType  string
	Tier         string
	LastModified time.Time
	ETag         string
	Leased       bool
	Snapshot     string
	VersionID    string
}

// BlobPage is one server page of entries plus its continuation marker.
type BlobPage struct {
	Entries []BlobEntry
	Marker  string // empty when the listing is exhausted
}

// ListContainers returns every container, alphabetically.
func (c *Clients) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	client, err := c.Blob()
	if err != nil {
		return nil, err
	}

	var out []ContainerInfo
	pager := client.NewListContainersPager(&azblob.ListContainersOptions{
		Include: azblob.ListContainersInclude{Metadata: true},
	})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, wrapAzure("list containers", err)
		}
		for _, item := range page.ContainerItems {
			if item == nil || item.Name == nil {
				continue
			}
			info := ContainerInfo{Name: *item.Name}
			if item.Properties != nil {
				info.LastModified = deref(item.Properties.LastModified)
				if item.Properties.PublicAccess != nil {
					info.Access = string(*item.Properties.PublicAccess)
				} else {
					info.Access = "private"
				}
				if item.Properties.LeaseState != nil {
					info.LeaseState = string(*item.Properties.LeaseState)
				}
			}
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ListBlobs returns one page of a hierarchical listing under prefix.
// Filtering is server-side via the prefix, so a container with a
// million blobs never lands in memory.
func (c *Clients) ListBlobs(ctx context.Context, containerName, prefix, marker string, pageSize int32) (BlobPage, error) {
	client, err := c.Blob()
	if err != nil {
		return BlobPage{}, err
	}
	if pageSize <= 0 {
		pageSize = 100
	}

	cc := client.ServiceClient().NewContainerClient(containerName)
	opts := &container.ListBlobsHierarchyOptions{
		MaxResults: &pageSize,
		Include:    container.ListBlobsInclude{Metadata: true},
	}
	if prefix != "" {
		opts.Prefix = &prefix
	}
	if marker != "" {
		opts.Marker = &marker
	}

	pager := cc.NewListBlobsHierarchyPager(Delimiter, opts)
	if !pager.More() {
		return BlobPage{}, nil
	}
	page, err := pager.NextPage(ctx)
	if err != nil {
		return BlobPage{}, wrapAzure("list blobs in "+containerName, err)
	}

	result := BlobPage{}
	if page.Segment != nil {
		for _, p := range page.Segment.BlobPrefixes {
			if p == nil || p.Name == nil {
				continue
			}
			result.Entries = append(result.Entries, BlobEntry{
				Name:    *p.Name,
				Display: leaf(*p.Name, prefix),
				IsDir:   true,
			})
		}
		for _, b := range page.Segment.BlobItems {
			if b == nil || b.Name == nil {
				continue
			}
			result.Entries = append(result.Entries, toEntry(b, prefix))
		}
	}
	// A blob listing already arrives sorted, but prefixes and blobs come
	// in separate arrays; interleaving them would scatter directories
	// through the table, so directories are kept first.
	if page.NextMarker != nil && *page.NextMarker != "" {
		result.Marker = *page.NextMarker
	}
	return result, nil
}

// ListBlobsFlat returns one page of a flat listing. Seed dump needs
// this: a hierarchical walk would miss every blob sitting under a
// virtual directory.
func (c *Clients) ListBlobsFlat(ctx context.Context, containerName, prefix, marker string, pageSize int32) (BlobPage, error) {
	client, err := c.Blob()
	if err != nil {
		return BlobPage{}, err
	}
	if pageSize <= 0 {
		pageSize = 100
	}

	cc := client.ServiceClient().NewContainerClient(containerName)
	opts := &container.ListBlobsFlatOptions{
		MaxResults: &pageSize,
		Include:    container.ListBlobsInclude{Metadata: true},
	}
	if prefix != "" {
		opts.Prefix = &prefix
	}
	if marker != "" {
		opts.Marker = &marker
	}

	pager := cc.NewListBlobsFlatPager(opts)
	if !pager.More() {
		return BlobPage{}, nil
	}
	page, err := pager.NextPage(ctx)
	if err != nil {
		return BlobPage{}, wrapAzure("list blobs in "+containerName, err)
	}

	result := BlobPage{}
	if page.Segment != nil {
		for _, b := range page.Segment.BlobItems {
			if b == nil || b.Name == nil {
				continue
			}
			result.Entries = append(result.Entries, toEntry(b, prefix))
		}
	}
	if page.NextMarker != nil && *page.NextMarker != "" {
		result.Marker = *page.NextMarker
	}
	return result, nil
}

func toEntry(b *container.BlobItem, prefix string) BlobEntry {
	e := BlobEntry{
		Name:    *b.Name,
		Display: leaf(*b.Name, prefix),
	}
	if b.Snapshot != nil {
		e.Snapshot = *b.Snapshot
	}
	if b.VersionID != nil {
		e.VersionID = *b.VersionID
	}
	if p := b.Properties; p != nil {
		e.Size = deref(p.ContentLength)
		e.LastModified = deref(p.LastModified)
		e.ContentType = deref(p.ContentType)
		if p.BlobType != nil {
			e.BlobType = strings.TrimSuffix(string(*p.BlobType), "Blob")
		}
		if p.AccessTier != nil {
			e.Tier = string(*p.AccessTier)
		}
		if p.LeaseStatus != nil {
			e.Leased = string(*p.LeaseStatus) == "locked"
		}
		if p.ETag != nil {
			e.ETag = string(*p.ETag)
		}
	}
	return e
}

// leaf strips the current prefix (and any trailing slash) so the table
// shows "order-001.json" rather than "archive/2026/order-001.json".
func leaf(full, prefix string) string {
	name := strings.TrimPrefix(full, prefix)
	name = strings.TrimSuffix(name, Delimiter)
	if name == "" {
		return full
	}
	return name
}

// SearchBlobs walks every container looking for names containing needle.
// It is the deep search behind Ctrl+F, and is deliberately bounded: an
// unbounded scan of a real storage account would run for hours.
func (c *Clients) SearchBlobs(ctx context.Context, needle string, limit int) ([]BlobEntry, error) {
	client, err := c.Blob()
	if err != nil {
		return nil, err
	}
	containers, err := c.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	needle = strings.ToLower(needle)

	var out []BlobEntry
	for _, ci := range containers {
		pager := client.NewListBlobsFlatPager(ci.Name, nil)
		for pager.More() {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			page, err := pager.NextPage(ctx)
			if err != nil {
				return out, wrapAzure("search "+ci.Name, err)
			}
			for _, b := range page.Segment.BlobItems {
				if b == nil || b.Name == nil || !strings.Contains(strings.ToLower(*b.Name), needle) {
					continue
				}
				e := toEntry(b, "")
				e.Display = ci.Name + "/" + *b.Name
				out = append(out, e)
				if len(out) >= limit {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

// CreateContainer creates a container, treating "already exists" as
// success so the seed applier is idempotent.
func (c *Clients) CreateContainer(ctx context.Context, name string, publicAccess string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	client, err := c.Blob()
	if err != nil {
		return err
	}
	opts := &azblob.CreateContainerOptions{}
	switch publicAccess {
	case "blob":
		opts.Access = to.Ptr(azblob.PublicAccessTypeBlob)
	case "container":
		opts.Access = to.Ptr(azblob.PublicAccessTypeContainer)
	}
	_, err = client.CreateContainer(ctx, name, opts)
	if isAlreadyExists(err) {
		return nil
	}
	return wrapAzure("create container "+name, err)
}

// DeleteContainer removes a container and everything in it.
func (c *Clients) DeleteContainer(ctx context.Context, name string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	client, err := c.Blob()
	if err != nil {
		return err
	}
	_, err = client.DeleteContainer(ctx, name, nil)
	return wrapAzure("delete container "+name, err)
}

// DeleteBlob removes one blob, including any snapshots it owns —
// Azure refuses to delete a blob with snapshots otherwise, and that
// error is far from obvious in a TUI.
func (c *Clients) DeleteBlob(ctx context.Context, containerName, blobName string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	client, err := c.Blob()
	if err != nil {
		return err
	}
	_, err = client.DeleteBlob(ctx, containerName, blobName, &azblob.DeleteBlobOptions{
		DeleteSnapshots: to.Ptr(azblob.DeleteSnapshotsOptionTypeInclude),
	})
	return wrapAzure("delete "+containerName+"/"+blobName, err)
}

// BlobProperties is the detail shown by the properties editor.
type BlobProperties struct {
	ContentType     string
	ContentEncoding string
	CacheControl    string
	Size            int64
	ETag            string
	LastModified    time.Time
	Tier            string
	LeaseState      string
	LeaseStatus     string
	Metadata        map[string]string
}

// Properties fetches a blob's properties and metadata.
func (c *Clients) Properties(ctx context.Context, containerName, blobName string) (BlobProperties, error) {
	bc, err := c.blobClient(containerName, blobName)
	if err != nil {
		return BlobProperties{}, err
	}
	resp, err := bc.GetProperties(ctx, nil)
	if err != nil {
		return BlobProperties{}, wrapAzure("properties of "+blobName, err)
	}

	p := BlobProperties{
		ContentType:     deref(resp.ContentType),
		ContentEncoding: deref(resp.ContentEncoding),
		CacheControl:    deref(resp.CacheControl),
		Size:            deref(resp.ContentLength),
		LastModified:    deref(resp.LastModified),
		Tier:            deref(resp.AccessTier),
		Metadata:        map[string]string{},
	}
	if resp.ETag != nil {
		p.ETag = string(*resp.ETag)
	}
	if resp.LeaseState != nil {
		p.LeaseState = string(*resp.LeaseState)
	}
	if resp.LeaseStatus != nil {
		p.LeaseStatus = string(*resp.LeaseStatus)
	}
	for k, v := range resp.Metadata {
		p.Metadata[k] = deref(v)
	}
	return p, nil
}

// SetMetadata replaces a blob's metadata.
func (c *Clients) SetMetadata(ctx context.Context, containerName, blobName string, md map[string]string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	bc, err := c.blobClient(containerName, blobName)
	if err != nil {
		return err
	}
	converted := make(map[string]*string, len(md))
	for k, v := range md {
		converted[k] = ptr(v)
	}
	_, err = bc.SetMetadata(ctx, converted, nil)
	return wrapAzure("set metadata on "+blobName, err)
}

// SetContentType updates a blob's Content-Type header. The SDK replaces
// the whole header set, so the other headers are read back first rather
// than being silently cleared.
func (c *Clients) SetContentType(ctx context.Context, containerName, blobName, contentType string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	bc, err := c.blobClient(containerName, blobName)
	if err != nil {
		return err
	}
	current, err := bc.GetProperties(ctx, nil)
	if err != nil {
		return wrapAzure("read headers of "+blobName, err)
	}
	_, err = bc.SetHTTPHeaders(ctx, blob.HTTPHeaders{
		BlobContentType:        ptr(contentType),
		BlobContentEncoding:    current.ContentEncoding,
		BlobContentLanguage:    current.ContentLanguage,
		BlobContentDisposition: current.ContentDisposition,
		BlobCacheControl:       current.CacheControl,
	}, nil)
	return wrapAzure("set content type on "+blobName, err)
}

// SetTier moves a blob between Hot, Cool, and Archive.
func (c *Clients) SetTier(ctx context.Context, containerName, blobName, tier string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	bc, err := c.blobClient(containerName, blobName)
	if err != nil {
		return err
	}
	_, err = bc.SetTier(ctx, blob.AccessTier(tier), nil)
	return wrapAzure("set tier on "+blobName, err)
}

// BreakLease forcibly releases a blob lease so a stuck local run can be
// cleaned up.
func (c *Clients) BreakLease(ctx context.Context, containerName, blobName string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	client, err := c.Blob()
	if err != nil {
		return err
	}
	lc, err := lease.NewBlobClient(client.ServiceClient().NewContainerClient(containerName).NewBlobClient(blobName), nil)
	if err != nil {
		return err
	}
	// BreakPeriod 0 ends the lease immediately rather than letting it
	// run out its remaining term, which is the point of a manual break.
	_, err = lc.BreakLease(ctx, &lease.BlobBreakOptions{BreakPeriod: to.Ptr(int32(0))})
	return wrapAzure("break lease on "+blobName, err)
}

// CopyBlob server-side copies a blob. Azurite completes small copies
// synchronously, so no polling loop is needed for local work.
func (c *Clients) CopyBlob(ctx context.Context, srcContainer, srcBlob, dstContainer, dstBlob string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	client, err := c.Blob()
	if err != nil {
		return err
	}
	src := client.ServiceClient().NewContainerClient(srcContainer).NewBlobClient(srcBlob)
	dst := client.ServiceClient().NewContainerClient(dstContainer).NewBlobClient(dstBlob)
	_, err = dst.StartCopyFromURL(ctx, src.URL(), nil)
	return wrapAzure("copy "+srcBlob, err)
}

// MoveBlob is a copy followed by a delete. It refuses a no-op move
// rather than copying a blob onto itself and then deleting it.
func (c *Clients) MoveBlob(ctx context.Context, srcContainer, srcBlob, dstContainer, dstBlob string) error {
	if srcContainer == dstContainer && srcBlob == dstBlob {
		return errors.New("source and destination are the same")
	}
	if err := c.CopyBlob(ctx, srcContainer, srcBlob, dstContainer, dstBlob); err != nil {
		return err
	}
	return c.DeleteBlob(ctx, srcContainer, srcBlob)
}

// SASOptions configures a generated SAS URL.
type SASOptions struct {
	Read   bool
	Write  bool
	Delete bool
	List   bool
	Add    bool
	Create bool
	Expiry time.Duration
}

// BlobSAS generates a SAS URL for one blob. It only works for shared-key
// profiles: a SAS cannot be signed without the account key, and Entra
// profiles would need a user-delegation key instead.
func (c *Clients) BlobSAS(containerName, blobName string, opts SASOptions) (string, error) {
	if c.profile.Auth != config.AuthSharedKey {
		return "", errors.New("SAS generation needs a shared-key profile")
	}
	bc, err := c.blobClient(containerName, blobName)
	if err != nil {
		return "", err
	}
	if opts.Expiry <= 0 {
		opts.Expiry = time.Hour
	}
	perms := sas.BlobPermissions{
		Read: opts.Read, Write: opts.Write, Delete: opts.Delete,
		Add: opts.Add, Create: opts.Create,
	}
	// Start slightly in the past: clock skew between the signer and the
	// service otherwise rejects a SAS for its first few seconds.
	return bc.GetSASURL(perms, time.Now().UTC().Add(opts.Expiry), &blob.GetSASURLOptions{
		StartTime: to.Ptr(time.Now().UTC().Add(-5 * time.Minute)),
	})
}

// URL returns the addressable URL of a blob, for yanking.
func (c *Clients) URL(containerName, blobName string) (string, error) {
	bc, err := c.blobClient(containerName, blobName)
	if err != nil {
		return "", err
	}
	return bc.URL(), nil
}

// ListVersions returns the snapshots of a blob, newest first.
func (c *Clients) ListVersions(ctx context.Context, containerName, blobName string) ([]BlobEntry, error) {
	client, err := c.Blob()
	if err != nil {
		return nil, err
	}
	cc := client.ServiceClient().NewContainerClient(containerName)
	pager := cc.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix:  &blobName,
		Include: container.ListBlobsInclude{Snapshots: true, Versions: true},
	})

	var out []BlobEntry
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, wrapAzure("list versions of "+blobName, err)
		}
		for _, b := range page.Segment.BlobItems {
			if b == nil || b.Name == nil || *b.Name != blobName {
				continue
			}
			out = append(out, toEntry(b, ""))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastModified.After(out[j].LastModified) })
	return out, nil
}

// blobClient builds a per-blob client.
func (c *Clients) blobClient(containerName, blobName string) (*blob.Client, error) {
	client, err := c.Blob()
	if err != nil {
		return nil, err
	}
	return client.ServiceClient().NewContainerClient(containerName).NewBlobClient(blobName), nil
}

// JoinPrefix builds a blob name from a virtual directory and a leaf,
// normalising the separator. Blob names use "/" on every platform, so a
// Windows filepath.Join here would produce unreachable names.
func JoinPrefix(prefix, name string) string {
	prefix = strings.TrimSuffix(prefix, Delimiter)
	name = strings.TrimPrefix(filepath.ToSlash(name), Delimiter)
	if prefix == "" {
		return name
	}
	return prefix + Delimiter + name
}

// ParentPrefix returns the prefix one level up, for Backspace.
func ParentPrefix(prefix string) string {
	trimmed := strings.TrimSuffix(prefix, Delimiter)
	idx := strings.LastIndex(trimmed, Delimiter)
	if idx < 0 {
		return ""
	}
	return trimmed[:idx+1]
}

// GuessContentType picks a Content-Type from a filename so uploaded
// JSON previews as JSON rather than as an octet-stream hex dump.
func GuessContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".csv":
		return "text/csv"
	case ".txt", ".log":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

// ReadRange downloads the first n bytes of a blob, for previews. Files
// over the preview limit stream only their head rather than pulling a
// gigabyte through the terminal.
func (c *Clients) ReadRange(ctx context.Context, containerName, blobName string, n int64) ([]byte, bool, error) {
	bc, err := c.blobClient(containerName, blobName)
	if err != nil {
		return nil, false, err
	}
	resp, err := bc.DownloadStream(ctx, &blob.DownloadStreamOptions{
		Range: blob.HTTPRange{Offset: 0, Count: n},
	})
	if err != nil {
		return nil, false, wrapAzure("preview "+blobName, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, n))
	if err != nil {
		return nil, false, err
	}
	truncated := deref(resp.ContentLength) > int64(len(data))
	return data, truncated, nil
}

// WriteAll replaces a blob's content in one request. Used by the
// edit-in-$EDITOR flow, where the content is already in memory.
func (c *Clients) WriteAll(ctx context.Context, containerName, blobName string, data []byte, contentType string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	client, err := c.Blob()
	if err != nil {
		return err
	}
	if contentType == "" {
		contentType = GuessContentType(blobName)
	}
	_, err = client.UploadBuffer(ctx, containerName, blobName, data, &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: ptr(contentType)},
	})
	return wrapAzure("write "+blobName, err)
}

// DownloadToFile writes a blob to a local path, creating parent
// directories as needed.
func (c *Clients) DownloadToFile(ctx context.Context, containerName, blobName, dst string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	client, err := c.Blob()
	if err != nil {
		return 0, err
	}
	n, err := client.DownloadFile(ctx, containerName, blobName, f, nil)
	if err != nil {
		os.Remove(dst) // never leave a half-written file that looks complete
		return 0, wrapAzure("download "+blobName, err)
	}
	return n, nil
}

// LocalDownloadPath builds a collision-free destination under dir.
func LocalDownloadPath(dir, blobName string) string {
	base := filepath.Join(dir, filepath.FromSlash(blobName))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return base
}
