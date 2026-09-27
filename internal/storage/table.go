package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
)

// TableInfo summarises one table for the sidebar.
type TableInfo struct {
	Name string
}

// Entity is a table row, flattened for display. Values keep their JSON
// types so the column chooser can show "Double" rather than "string".
type Entity struct {
	PartitionKey string
	RowKey       string
	Timestamp    time.Time
	ETag         string
	Properties   map[string]any
}

// EntityPage is one page of a query plus its continuation token.
type EntityPage struct {
	Entities []Entity
	Columns  []string // union of property names across the page
	Token    string   // empty when exhausted
}

// ListTables returns every table, alphabetically.
func (c *Clients) ListTables(ctx context.Context) ([]TableInfo, error) {
	svc, err := c.Table()
	if err != nil {
		return nil, err
	}

	var out []TableInfo
	pager := svc.NewListTablesPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, wrapAzure("list tables", err)
		}
		for _, t := range page.Tables {
			if t == nil || t.Name == nil {
				continue
			}
			out = append(out, TableInfo{Name: *t.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// QueryEntities runs an OData filter and returns one page.
//
// Paging is server-side via the continuation token rather than a
// client-side slice: a table with a million rows must not be pulled
// into memory to show a hundred.
func (c *Clients) QueryEntities(ctx context.Context, table, filter, token string, pageSize int32) (EntityPage, error) {
	svc, err := c.Table()
	if err != nil {
		return EntityPage{}, err
	}
	if pageSize <= 0 {
		pageSize = 100
	}

	opts := &aztables.ListEntitiesOptions{Top: &pageSize}
	if strings.TrimSpace(filter) != "" {
		opts.Filter = to.Ptr(filter)
	}
	// aztables splits the continuation across two headers; we carry them
	// as one opaque "pk|rk" string so the UI only tracks a single value.
	if token != "" {
		pk, rk, found := strings.Cut(token, "|")
		if !found {
			return EntityPage{}, fmt.Errorf("malformed continuation token")
		}
		opts.NextPartitionKey, opts.NextRowKey = &pk, &rk
	}

	pager := svc.NewClient(table).NewListEntitiesPager(opts)
	if !pager.More() {
		return EntityPage{}, nil
	}
	page, err := pager.NextPage(ctx)
	if err != nil {
		return EntityPage{}, wrapAzure("query "+table, err)
	}

	result := EntityPage{}
	columnSet := map[string]bool{}
	for _, raw := range page.Entities {
		e, err := parseEntity(raw)
		if err != nil {
			continue // one unparseable row must not fail the whole page
		}
		for k := range e.Properties {
			columnSet[k] = true
		}
		result.Entities = append(result.Entities, e)
	}

	// PartitionKey and RowKey lead; everything else is alphabetical, so
	// column order does not jitter between pages.
	for k := range columnSet {
		if k != "PartitionKey" && k != "RowKey" {
			result.Columns = append(result.Columns, k)
		}
	}
	sort.Strings(result.Columns)
	result.Columns = append([]string{"PartitionKey", "RowKey"}, result.Columns...)

	if page.NextPartitionKey != nil && page.NextRowKey != nil {
		result.Token = *page.NextPartitionKey + "|" + *page.NextRowKey
	}
	return result, nil
}

func parseEntity(raw []byte) (Entity, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return Entity{}, err
	}

	e := Entity{Properties: map[string]any{}}
	for k, v := range m {
		switch k {
		case "PartitionKey":
			e.PartitionKey, _ = v.(string)
		case "RowKey":
			e.RowKey, _ = v.(string)
		case "Timestamp":
			if s, ok := v.(string); ok {
				e.Timestamp, _ = time.Parse(time.RFC3339Nano, s)
			}
			continue
		case "odata.etag":
			e.ETag, _ = v.(string)
			continue
		}
		// The OData type annotations (`Total@odata.type`) describe the
		// sibling property rather than being data of their own.
		if strings.Contains(k, "@odata.") {
			continue
		}
		e.Properties[k] = v
	}
	e.Properties["PartitionKey"] = e.PartitionKey
	e.Properties["RowKey"] = e.RowKey
	return e, nil
}

// GetEntity fetches one row.
func (c *Clients) GetEntity(ctx context.Context, table, partitionKey, rowKey string) (Entity, error) {
	svc, err := c.Table()
	if err != nil {
		return Entity{}, err
	}
	resp, err := svc.NewClient(table).GetEntity(ctx, partitionKey, rowKey, nil)
	if err != nil {
		return Entity{}, wrapAzure("get entity", err)
	}
	return parseEntity(resp.Value)
}

// UpsertEntity inserts or replaces a row, which is what makes the seed
// applier idempotent.
func (c *Clients) UpsertEntity(ctx context.Context, table string, props map[string]any) error {
	if err := c.Guard(); err != nil {
		return err
	}
	if _, ok := props["PartitionKey"]; !ok {
		return fmt.Errorf("entity needs a PartitionKey")
	}
	if _, ok := props["RowKey"]; !ok {
		return fmt.Errorf("entity needs a RowKey")
	}
	svc, err := c.Table()
	if err != nil {
		return err
	}
	// Timestamp and ETag are service-owned; sending them back is
	// rejected as a malformed entity.
	clean := make(map[string]any, len(props))
	for k, v := range props {
		if k == "Timestamp" || k == "odata.etag" || strings.Contains(k, "@odata.") {
			continue
		}
		clean[k] = v
	}
	body, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	_, err = svc.NewClient(table).UpsertEntity(ctx, body, nil)
	return wrapAzure("upsert entity", err)
}

// MergeEntity patches named properties, leaving the rest intact.
func (c *Clients) MergeEntity(ctx context.Context, table string, props map[string]any) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Table()
	if err != nil {
		return err
	}
	body, err := json.Marshal(props)
	if err != nil {
		return err
	}
	_, err = svc.NewClient(table).UpsertEntity(ctx, body, &aztables.UpsertEntityOptions{
		UpdateMode: aztables.UpdateModeMerge,
	})
	return wrapAzure("merge entity", err)
}

// DeleteEntity removes one row.
func (c *Clients) DeleteEntity(ctx context.Context, table, partitionKey, rowKey string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Table()
	if err != nil {
		return err
	}
	_, err = svc.NewClient(table).DeleteEntity(ctx, partitionKey, rowKey, nil)
	return wrapAzure("delete entity", err)
}

// CreateTable creates a table idempotently.
func (c *Clients) CreateTable(ctx context.Context, name string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Table()
	if err != nil {
		return err
	}
	_, err = svc.CreateTable(ctx, name, nil)
	if isAlreadyExists(err) {
		return nil
	}
	return wrapAzure("create table "+name, err)
}

// DeleteTable removes a table and every entity in it.
func (c *Clients) DeleteTable(ctx context.Context, name string) error {
	if err := c.Guard(); err != nil {
		return err
	}
	svc, err := c.Table()
	if err != nil {
		return err
	}
	_, err = svc.DeleteTable(ctx, name, nil)
	return wrapAzure("delete table "+name, err)
}

// ValidateFilter catches the OData mistakes that otherwise come back as
// an opaque 400 from the service, before a keystroke is wasted on a
// round trip.
func ValidateFilter(filter string) error {
	f := strings.TrimSpace(filter)
	if f == "" {
		return nil
	}
	if strings.Count(f, "'")%2 != 0 {
		return fmt.Errorf("unbalanced quote")
	}
	if strings.Count(f, "(") != strings.Count(f, ")") {
		return fmt.Errorf("unbalanced parenthesis")
	}
	// `=` is the single most common mistake for anyone arriving from
	// SQL; OData spells equality `eq`.
	if strings.Contains(f, "==") || (strings.Contains(f, "=") && !strings.Contains(f, ">=") && !strings.Contains(f, "<=")) {
		return fmt.Errorf("OData uses `eq`, not `=`")
	}
	hasOperator := false
	for _, op := range []string{" eq ", " ne ", " gt ", " ge ", " lt ", " le "} {
		if strings.Contains(strings.ToLower(f), op) {
			hasOperator = true
			break
		}
	}
	if !hasOperator {
		return fmt.Errorf("expected a comparison such as `PartitionKey eq '2026'`")
	}
	return nil
}

// TypeName renders a property's OData type for the entity detail pane.
func TypeName(v any) string {
	switch t := v.(type) {
	case nil:
		return "Null"
	case bool:
		return "Boolean"
	case float64:
		if t == float64(int64(t)) {
			return "Int64"
		}
		return "Double"
	case string:
		return "String"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// FormatValue renders a property value as a single display cell.
func FormatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}
