package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()
	ok, err := SafeJoin(root, "blob/a.json")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(ok) != filepath.Join(root, "blob") {
		t.Fatalf("got %s", ok)
	}
	if _, err := SafeJoin(root, "../etc/passwd"); err == nil {
		t.Fatal("expected traversal to fail")
	}
	if _, err := SafeJoin(root, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute member to fail")
	}
	if _, err := SafeJoin(root, "foo/../../etc/passwd"); err == nil {
		t.Fatal("expected nested traversal to fail")
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	if err := WriteAtomic(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestBackoff(t *testing.T) {
	if Backoff(0, 1, 100) != 1 {
		t.Fatal("attempt 0")
	}
	if Backoff(40, 1, 10) != 10 {
		t.Fatal("should cap")
	}
}
