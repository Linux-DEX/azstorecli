package keymap

import "testing"

func TestDefaultsNoConflict(t *testing.T) {
	km := Defaults()
	if err := km.DetectConflicts(); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownAction(t *testing.T) {
	km := Defaults()
	if err := km.SetKeys("does.not.exist", []string{"x"}); err == nil {
		t.Fatal("expected unknown action")
	}
}

func TestLookup(t *testing.T) {
	if _, ok := Lookup("app.quit"); !ok {
		t.Fatal("app.quit missing")
	}
}

func TestConflict(t *testing.T) {
	km := Defaults()
	if err := km.SetKeys("app.quit", []string{"ctrl+c"}); err != nil {
		t.Fatal(err)
	}
	if err := km.DetectConflicts(); err == nil {
		t.Fatal("ctrl+c is already force_quit")
	}
}
