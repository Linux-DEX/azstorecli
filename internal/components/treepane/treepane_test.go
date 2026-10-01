package treepane

import (
	"strings"
	"testing"

	"github.com/Linux-DEX/azstorecli/internal/theme"
)

func TestSelectedNameStaysVisible(t *testing.T) {
	m := New(theme.Dark(), "Containers")
	m.Focused = true
	m.SetSize(28, 4)
	m.SetItems([]Item{
		{Name: "orders", Badge: "private"},
		{Name: "inbox", Count: 3, ShowCount: true},
	})
	view := m.View()
	if !strings.Contains(view, "orders") {
		t.Fatalf("selected container name missing:\n%s", view)
	}
	m.Select("inbox")
	view = m.View()
	if !strings.Contains(view, "inbox") {
		t.Fatalf("selected queue name missing:\n%s", view)
	}
}
