package funcs

import "testing"

func TestParseRoutes(t *testing.T) {
	lines := []string{
		"Functions:",
		"",
		"        HttpCreateOrder: [POST] http://localhost:7071/api/orders",
		"        OrderCreated: queueTrigger",
		"",
		"Host started",
	}
	got := ParseRoutes(lines)
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	by := map[string]Function{}
	for _, f := range got {
		by[f.Name] = f
	}
	if by["HttpCreateOrder"].URL != "http://localhost:7071/api/orders" {
		t.Fatal(by["HttpCreateOrder"])
	}
	if by["OrderCreated"].Trigger != "queueTrigger" {
		t.Fatal(by["OrderCreated"])
	}
}

func TestMergeHostWinsURL(t *testing.T) {
	disk := []Function{{Name: "HttpHealth", Trigger: "httpTrigger", Source: "src/health.ts"}}
	host := []Function{{Name: "HttpHealth", Trigger: "httpTrigger", URL: "http://127.0.0.1:7071/api/health", Methods: []string{"GET"}}}
	got := Merge(disk, host)
	if len(got) != 1 || got[0].URL == "" || got[0].Source == "" {
		t.Fatalf("%+v", got)
	}
}
