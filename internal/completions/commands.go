package completions

import (
	"strings"

	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
)

type commandsGroup struct {
	registry *dialog.CommandRegistry
}

func (cg *commandsGroup) GetId() string {
	return "command"
}

func (cg *commandsGroup) GetEntry() dialog.CompletionItemI {
	return dialog.NewCompletionItem(dialog.CompletionItem{
		Title: "Commands",
		Value: "commands",
	})
}

// GetChildEntries lists the registered commands whose ID starts with query,
// in registration order, as "/id" values ready to replace the typed prefix.
func (cg *commandsGroup) GetChildEntries(query string) ([]dialog.CompletionItemI, error) {
	var items []dialog.CompletionItemI
	for _, cmd := range cg.registry.Commands() {
		if !strings.HasPrefix(cmd.ID, query) {
			continue
		}
		items = append(items, dialog.NewCompletionItem(dialog.CompletionItem{
			Title: cmd.Title,
			Value: "/" + cmd.ID,
		}))
	}
	return items, nil
}

func NewCommandsGroup(registry *dialog.CommandRegistry) dialog.CompletionProvider {
	return &commandsGroup{registry: registry}
}
