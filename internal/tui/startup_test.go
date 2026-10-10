package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
)

// printedLines pulls out what a command tree hands to the terminal, the way
// Bubble Tea's renderer does: tea.Println produces an unexported
// printLineMessage whose body the renderer splits into lines and writes above
// the rendered view.
func printedLines(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []string
		for _, c := range batch {
			out = append(out, printedLines(c)...)
		}
		return out
	}
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Struct || v.Type().Name() != "printLineMessage" || v.NumField() != 1 {
		return nil
	}
	return strings.Split(v.Field(0).String(), "\n")
}

func startupModel(t *testing.T) (tea.Model, *app.App) {
	t.Helper()
	dir := t.TempDir()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	lipgloss.SetColorProfile(0)

	conn := dbtest.New(t)
	q := db.New(conn)
	a := &app.App{
		Sessions:   session.NewService(q),
		Messages:   message.NewService(q),
		CoderAgent: idleAgent{},
		LSPClients: map[string]*lsp.Client{},
	}
	m := New(a)
	m.Init()
	return m, a
}

// Aux takes the whole terminal at startup without taking the alternate
// screen: the banner is printed into the scrollback and padded so the live
// region lands on the last row. The user asked for the terminal to be filled;
// the alternate screen is what the rest of this work removed, because it costs
// the scrollback, the wheel and text selection.
//
// This is where a regression would show as a composer floating in the middle
// of the screen, or as a screen that scrolled past the banner.
func TestTheStartupBannerFillsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{70, 20}, {80, 24}, {100, 30}, {120, 36}, {160, 44}, {200, 50}} {
		width, height := size[0], size[1]
		t.Run(strings.TrimSpace(strings.Join([]string{itoa(width), itoa(height)}, "x")), func(t *testing.T) {
			m, _ := startupModel(t)
			m, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: height})

			printed := printedLines(cmd)
			if len(printed) == 0 {
				t.Fatal("nothing was printed at startup, so there is no banner")
			}
			live := strings.Split(m.View(), "\n")
			total := len(printed) + len(live)
			if total != height {
				t.Errorf("startup occupies %d rows (%d printed + %d live) of a %d-row terminal",
					total, len(printed), len(live), height)
			}
			for i, line := range printed {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("printed row %d is %d cells wide, %d over", i, w, w-width)
					break
				}
			}
		})
	}
}

// A printed line belongs to the terminal and cannot be withdrawn, so a banner
// printed twice leaves two of them in the user's scrollback. Resizing is the
// obvious way to trigger it: the size report is what prompts the print.
func TestTheStartupBannerIsPrintedOnce(t *testing.T) {
	m, _ := startupModel(t)
	m, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if len(printedLines(cmd)) == 0 {
		t.Fatal("test setup: the banner was not printed on the first size report")
	}

	for _, size := range [][2]int{{100, 30}, {80, 24}, {120, 40}} {
		var again tea.Cmd
		m, again = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if lines := printedLines(again); len(lines) != 0 {
			t.Fatalf("resizing to %dx%d printed the banner again: %q", size[0], size[1], lines)
		}
	}
}

// The composer is ruled above and below. Measuring Claude Code showed the same
// two-rule frame around its prompt, and it is what makes the live region read
// as one surface rather than as text that happens to be at the bottom.
func TestTheComposerIsRuledAboveAndBelow(t *testing.T) {
	m, _ := startupModel(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	lines := strings.Split(ansi.Strip(m.View()), "\n")
	composer := -1
	for i, line := range lines {
		if strings.Contains(line, ">") {
			composer = i
			break
		}
	}
	if composer < 0 {
		t.Fatalf("the composer is not in the live region: %q", lines)
	}
	isRule := func(i int) bool {
		return i >= 0 && i < len(lines) && strings.Count(lines[i], "─") > 10
	}
	if !isRule(composer - 1) {
		t.Errorf("no rule above the composer; row %d is %q", composer-1, safeRow(lines, composer-1))
	}
	ruledBelow := false
	for i := composer + 1; i < len(lines) && i <= composer+3; i++ {
		if isRule(i) {
			ruledBelow = true
			break
		}
	}
	if !ruledBelow {
		t.Errorf("no rule below the composer; rows after it are %q", lines[min(composer+1, len(lines)-1):])
	}
}

// The run information goes under the composer, not over it. Above the
// composer belongs to the conversation, which is printed: a header rendered
// there put Aux's own repainted rows in between the printed history and the
// prompt.
func TestTheHeaderIsBelowTheComposer(t *testing.T) {
	m, _ := startupModel(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	lines := strings.Split(ansi.Strip(m.View()), "\n")
	composer, header := -1, -1
	for i, line := range lines {
		if composer < 0 && strings.Contains(line, ">") {
			composer = i
		}
		// The header is the only row naming the context window.
		if header < 0 && strings.Contains(line, "Context") {
			header = i
		}
	}
	if composer < 0 {
		t.Fatalf("no composer in the live region: %q", lines)
	}
	if header < 0 {
		t.Skip("no header rendered at this size, so there is nothing to order")
	}
	if header < composer {
		t.Errorf("the header is on row %d, above the composer on row %d", header, composer)
	}
}

func safeRow(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return "(off the top of the region)"
	}
	return lines[i]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Opening an overlay grows the managed region to the whole screen (#53). The
// composer has to stay where it was -- at the bottom -- while that happens.
// Growing the region by padding underneath instead put the composer at the top
// of the screen and left the dialog floating below it, which is what the real
// binary did on a first run with the init dialog up.
func TestAnOverlayDoesNotMoveTheComposerToTheTop(t *testing.T) {
	const height = 30
	m, _ := startupModel(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: height})

	am, ok := m.(appModel)
	if !ok {
		t.Fatalf("expected an appModel, got %T", m)
	}

	for _, tc := range []struct {
		name string
		set  func(*appModel)
	}{
		{"help", func(a *appModel) { a.showHelp = true }},
		{"init dialog", func(a *appModel) { a.showInitDialog = true }},
		{"quit confirmation", func(a *appModel) { a.showQuit = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			with := am
			tc.set(&with)
			lines := strings.Split(ansi.Strip(with.View()), "\n")
			if len(lines) < height {
				t.Fatalf("the region is %d rows of %d; the overlay is clipped", len(lines), height)
			}
			composer := -1
			for i, line := range lines {
				if strings.Contains(line, "Describe a task") {
					composer = i
				}
			}
			if composer < 0 {
				t.Fatalf("the composer is not in the grown region at all")
			}
			// It belongs in the bottom few rows, where it is when no overlay
			// is up -- not at the top with the dialog hanging below it.
			if composer < len(lines)-6 {
				t.Errorf("the composer is on row %d of %d; it should be in the last rows",
					composer+1, len(lines))
			}
		})
	}
}

// On a first run the init dialog is already up when the first size report
// arrives. An overlay grows the managed region to the whole screen, so
// measuring the region through View made the padding come out as zero or
// negative -- and once the dialog was answered the region collapsed and left
// the bottom third of the terminal empty. Found by running the real binary
// in a pty, not by reading the code.
func TestTheFillIsRightEvenWhenADialogIsUpAtStartup(t *testing.T) {
	const height = 30
	var m tea.Model
	{
		base, _ := startupModel(t)
		// New returns a pointer; Update hands back a value.
		am, ok := base.(*appModel)
		if !ok {
			t.Fatalf("expected an *appModel, got %T", base)
		}
		am.showInitDialog = true
		m = am
	}

	m, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
	printed := printedLines(cmd)
	if len(printed) == 0 {
		t.Fatal("no banner was printed while the dialog was up")
	}

	// The user answers the dialog; the region collapses back.
	am, ok := m.(appModel)
	if !ok {
		t.Fatalf("expected an appModel, got %T", m)
	}
	am.showInitDialog = false

	total := len(printed) + len(strings.Split(am.View(), "\n"))
	if total != height {
		t.Errorf("after the dialog closed, startup occupies %d rows of a %d-row terminal", total, height)
	}
}
