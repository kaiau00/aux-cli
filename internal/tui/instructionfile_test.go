package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
)

// One name is the name Aux writes: AUX.md.
//
// Aux reads six spellings (config.defaultContextPaths), which is right for a
// project that already has a CLAUDE.md or an AGENTS.md. Writing a different
// one than it reads first is not: the old /init prompt asked for `Aux.md` and
// then told the model to improve `aux.md` if it existed, which on a
// case-sensitive filesystem are two files, and a user who had CLAUDE.md got a
// second instruction file instead of a better one.
func TestTheInitCommandWritesAUXmd(t *testing.T) {
	m := newCommandTestApp(t)
	am, ok := m.(*appModel)
	if !ok {
		t.Fatalf("expected an *appModel, got %T", m)
	}
	registered, found := am.commands.Find("init")
	if !found {
		t.Fatal("there is no init command")
	}

	if text := registered.Description; !strings.Contains(text, "AUX.md") {
		t.Errorf("the init command describes itself as writing something else: %q", text)
	}

	// The prompt itself is what the model acts on. /init sends it as a
	// SendMsg, so running the command's handler is how to read it.
	// tea.Batch with a single command returns that command rather than a
	// batch, so both shapes have to be unwrapped.
	var prompt string
	switch msg := registered.Handler(registered)().(type) {
	case chat.SendMsg:
		prompt = msg.Text
	case tea.BatchMsg:
		for _, c := range msg {
			if send, ok := c().(chat.SendMsg); ok {
				prompt = send.Text
			}
		}
	}
	if prompt == "" {
		t.Fatal("the init command sent no prompt")
	}
	if !strings.Contains(prompt, "AUX.md") {
		t.Errorf("the init prompt does not ask for AUX.md:\n%s", prompt)
	}
	if strings.Contains(prompt, "create an Aux.md") || strings.Contains(prompt, "already a aux.md") {
		t.Errorf("the init prompt still names a different file:\n%s", prompt)
	}
	// And it has to say what to do about the names Aux also reads, or it
	// writes a second file next to the one the project already had.
	for _, alias := range []string{"CLAUDE.md", "AGENTS.md"} {
		if !strings.Contains(prompt, alias) {
			t.Errorf("the init prompt ignores %s, which Aux reads:\n%s", alias, prompt)
		}
	}
}
