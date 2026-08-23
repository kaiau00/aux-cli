package dashboard

import (
	"fmt"
	"strings"

	"github.com/kaiau00/aux-cli/internal/history"
	"github.com/kaiau00/aux-cli/internal/message"
)

const snippetLimit = 220

type redactor struct {
	mode RedactionMode
}

func newRedactor(options Options) redactor {
	if options.FullContent {
		return redactor{mode: RedactionFull}
	}
	switch options.Redaction {
	case RedactionFull, RedactionMetadata:
		return redactor{mode: options.Redaction}
	default:
		return redactor{mode: RedactionRedacted}
	}
}

func (r redactor) message(msg message.Message) MessageDTO {
	dto := MessageDTO{
		ID:        msg.ID,
		SessionID: msg.SessionID,
		Role:      string(msg.Role),
		Model:     string(msg.Model),
		CreatedAt: msg.CreatedAt,
		UpdatedAt: msg.UpdatedAt,
	}
	if finish := msg.FinishPart(); finish != nil {
		dto.Finish = string(finish.Reason)
	}
	switch r.mode {
	case RedactionFull:
		dto.Text = msg.Content().String()
		dto.Reasoning = msg.ReasoningContent().String()
	case RedactionMetadata:
		dto.Text = ""
		dto.Reasoning = ""
	default:
		dto.Text = snippet(msg.Content().String())
		dto.Reasoning = redactedLabel(msg.ReasoningContent().String(), "reasoning")
	}
	for _, tc := range msg.ToolCalls() {
		dto.Tools = append(dto.Tools, r.toolCall(tc))
	}
	for _, tr := range msg.ToolResults() {
		dto.Results = append(dto.Results, r.toolResult(tr))
	}
	return dto
}

func (r redactor) toolCall(tc message.ToolCall) ToolCallDTO {
	dto := ToolCallDTO{
		ID:       tc.ID,
		Name:     tc.Name,
		Finished: tc.Finished,
	}
	switch r.mode {
	case RedactionFull:
		dto.Input = tc.Input
	case RedactionMetadata:
		dto.Input = ""
	default:
		dto.Input = redactedLabel(tc.Input, "tool input")
	}
	return dto
}

func (r redactor) toolResult(tr message.ToolResult) ToolResultDTO {
	dto := ToolResultDTO{
		ToolCallID: tr.ToolCallID,
		Name:       tr.Name,
		IsError:    tr.IsError,
	}
	switch r.mode {
	case RedactionFull:
		dto.Content = tr.Content
		dto.Metadata = tr.Metadata
	case RedactionMetadata:
		dto.Content = ""
		dto.Metadata = ""
	default:
		dto.Content = redactedLabel(tr.Content, "tool output")
		dto.Metadata = redactedLabel(tr.Metadata, "tool metadata")
	}
	return dto
}

func (r redactor) file(f history.File) FileDTO {
	dto := FileDTO{
		ID:        f.ID,
		SessionID: f.SessionID,
		Path:      f.Path,
		Version:   f.Version,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
	switch r.mode {
	case RedactionFull:
		dto.Content = f.Content
	case RedactionMetadata:
		dto.Content = ""
	default:
		dto.Content = redactedLabel(f.Content, "file content")
	}
	return dto
}

func snippet(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= snippetLimit {
		return value
	}
	return value[:snippetLimit] + "..."
}

func redactedLabel(value, label string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return fmt.Sprintf("[%s redacted]", label)
}
