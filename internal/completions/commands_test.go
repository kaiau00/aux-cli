package completions

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
)

var update = flag.Bool("update", false, "update golden files")

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func testRegistry() *dialog.CommandRegistry {
	r := dialog.NewCommandRegistry()
	for _, id := range []string{"init", "compact", "exclude", "help", "model", "sessions", "user:review"} {
		r.Register(dialog.Command{ID: id, Title: id})
	}
	return r
}

func TestCommandsGroupFiltersByPrefixInOrder(t *testing.T) {
	items, err := NewCommandsGroup(testRegistry()).GetChildEntries("s")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.GetValue())
	}
	if strings.Join(got, ",") != "/sessions" {
		t.Fatalf("got %v, want [/sessions]", got)
	}
}

// renderPopup types keys into the popup the way the chat page forwards them
// after "/" opens it, and returns the view without styling.
func renderPopup(keys string) string {
	c := dialog.NewCompletionDialogCmp(NewCommandsGroup(testRegistry()), "No matching command")
	c.SetWidth(40)
	var m tea.Model = c
	for _, r := range keys {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	lines := strings.Split(ansiRE.ReplaceAllString(m.View(), ""), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

func TestCommandPopupGolden(t *testing.T) {
	for name, keys := range map[string]string{
		"all":       "/",
		"prefix-co": "/co",
		"no-match":  "/zz",
	} {
		got := renderPopup(keys)
		golden := filepath.Join("testdata", "command-popup."+name+".golden")
		if *update {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("missing golden %s (run: go test ./internal/completions -update): %v", golden, err)
		}
		if got != string(want) {
			t.Fatalf("golden %s mismatch:\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
		}
	}
}
