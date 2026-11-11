package visual

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
)

// Reuse the shared ANSI stripping and golden update flag.
func TestCompactDialogGolden(t *testing.T) {
	for name, size := range map[string][2]int{"wide": {80, 24}, "narrow": {40, 14}} {
		d := dialog.NewCompactDialog([]dialog.CompactPage{
			{Name: "large.go", Tokens: 24000, ToolCallIDs: []string{"tool"}},
			{Name: "project_manifest", Tokens: 3000},
			{Name: "tests.go", Tokens: 2000, ToolCallIDs: []string{"test"}, Pinned: true},
			{Name: "msg:user", Tokens: 1000},
			{Name: "output", Tokens: 900, ToolCallIDs: []string{"out"}, Excluded: true},
		}, nil)
		d.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		got := plain(d.View())
		path := filepath.Join("testdata", "compact."+name+".golden")
		if *update {
			if err := os.WriteFile(path, []byte(got), 0644); err != nil {
				t.Fatal(err)
			}
		} else {
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != got {
				t.Fatalf("%s mismatch:\n%s", path, got)
			}
		}
		if lipgloss.Width(got) > size[0] || lipgloss.Height(got) > size[1] {
			t.Fatalf("dialog overflows %v", size)
		}
	}
}
