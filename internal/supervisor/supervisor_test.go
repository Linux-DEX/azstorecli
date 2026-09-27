package supervisor

import "testing"

func TestResolveOrder(t *testing.T) {
	s := New(nil)
	_ = s.Register(Spec{Name: "azurite"})
	_ = s.Register(Spec{Name: "functions", DependsOn: []string{"azurite"}})
	plan, err := s.resolve([]string{"functions"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0] != "azurite" || plan[1] != "functions" {
		t.Fatalf("%v", plan)
	}
}

func TestResolveCycle(t *testing.T) {
	s := New(nil)
	_ = s.Register(Spec{Name: "a", DependsOn: []string{"b"}})
	_ = s.Register(Spec{Name: "b", DependsOn: []string{"a"}})
	if _, err := s.resolve([]string{"a"}); err == nil {
		t.Fatal("expected cycle")
	}
}

func TestLogRingWrap(t *testing.T) {
	r := NewLogRing(3)
	for i := 0; i < 5; i++ {
		r.Append(LogLine{Text: string(rune('a' + i))})
	}
	lines := r.Lines()
	if len(lines) != 3 || lines[0].Text != "c" {
		t.Fatalf("%v", lines)
	}
	_, dropped := r.Stats()
	if dropped < 2 {
		t.Fatalf("dropped %d", dropped)
	}
}
