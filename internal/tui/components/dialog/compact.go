package dialog

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/tui/util"
)

// CompactPage is one page in the latest call, with the existing tool-result
// controls attached when that page can be excluded or pinned.
type CompactPage struct {
	Name             string
	Tokens           int64
	TaskID           string
	ToolCallIDs      []string
	Pinned, Excluded bool
}

type CompactChoiceMsg struct{ Compact bool }
type compactAppliedMsg struct {
	index int
	pin   bool
	err   error
}

type CompactDialog struct {
	Pages                   []CompactPage
	Apply                   func(CompactPage, bool) error
	selected, width, height int
	pending                 bool
	AutoCompact             bool
}

func NewCompactDialog(pages []CompactPage, apply func(CompactPage, bool) error) *CompactDialog {
	return &CompactDialog{Pages: pages, Apply: apply}
}
func (d *CompactDialog) Init() tea.Cmd { return nil }
func (d *CompactDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		d.width, d.height = m.Width, m.Height
	case compactAppliedMsg:
		d.pending = false
		if m.index < 0 || m.index >= len(d.Pages) {
			return d, nil
		}
		if m.err != nil {
			return d, util.ReportError(m.err)
		}
		if m.pin {
			d.Pages[m.index].Pinned = !d.Pages[m.index].Pinned
		} else {
			d.Pages[m.index].Excluded = true
		}
	case tea.KeyMsg:
		if d.pending {
			return d, nil
		}
		switch m.String() {
		case "up", "k":
			d.selected = max(0, d.selected-1)
		case "down", "j", "tab":
			d.selected = (d.selected + 1) % (len(d.Pages) + 2)
		case "esc":
			return d, util.CmdHandler(CompactChoiceMsg{})
		case "enter":
			if d.selected >= len(d.Pages) {
				return d, util.CmdHandler(CompactChoiceMsg{Compact: d.selected == len(d.Pages)})
			}
		case "x", "p":
			if d.selected >= len(d.Pages) {
				break
			}
			p := d.Pages[d.selected]
			if len(p.ToolCallIDs) == 0 {
				return d, util.ReportWarn("This page has no tool results to exclude or pin")
			}
			if d.Apply == nil {
				break
			}
			i, pin := d.selected, m.String() == "p"
			d.pending = true
			return d, func() tea.Msg { return compactAppliedMsg{i, pin, d.Apply(p, pin)} }
		}
	}
	return d, nil
}
func (d *CompactDialog) View() string {
	w := fitWidth(64, d.width)
	rows := fitRows(len(d.Pages)+4, d.height)
	items := make([]string, 0, len(d.Pages)+2)
	for _, p := range d.Pages {
		state := ""
		if p.Excluded {
			state += " [excluded]"
		}
		if p.Pinned {
			state += " [pinned]"
		}
		if len(p.ToolCallIDs) == 0 {
			state += " [fixed]"
		}
		items = append(items, fmt.Sprintf("%d tokens · %s%s", p.Tokens, p.Name, state))
	}
	continueLabel := "Continue anyway"
	if d.AutoCompact {
		continueLabel = "Continue (auto-compact)"
	}
	items = append(items, "Compact now (transcript)", continueLabel)
	// Scroll the selection into the available rows; actions remain reachable
	// even on short terminals rather than growing the managed region.
	visible := max(1, rows-2)
	start := max(0, d.selected-visible+1)
	lines := []string{"Context near capacity"}
	for i := start; i < min(len(items), start+visible); i++ {
		marker := "  "
		if i == d.selected {
			marker = "> "
		}
		lines = append(lines, ansi.Truncate(marker+items[i], w, "…"))
	}
	lines = append(lines, ansi.Truncate("x exclude/p pin: later prompts; Enter/Esc", w, "…"))
	t := theme.CurrentTheme()
	return styles.BaseStyle().Background(t.Background()).Foreground(t.Text()).
		Border(lipgloss.RoundedBorder()).BorderForeground(t.Primary()).Padding(1, 2).
		Width(w + 4).Render(strings.Join(lines, "\n"))
}
