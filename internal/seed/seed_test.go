package seed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "containers.yaml")
	body := []byte("version: 1\nblob:\n  containers:\n    - name: orders\n      blobs:\n        - name: a.json\n          content: '{}'\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Blob.Containers) != 1 {
		t.Fatal(doc)
	}
}

func TestValidateRejectsBothFileAndContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "containers.yaml")
	_ = os.WriteFile(path, []byte("version: 1\nblob:\n  containers:\n    - name: x\n      blobs:\n        - name: a\n          file: f\n          content: y\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestRejectsAbsoluteFixture(t *testing.T) {
	d := &Doc{dir: t.TempDir()}
	if _, err := d.resolve("/etc/passwd"); err == nil {
		t.Fatal("absolute fixture must be rejected")
	}
}
