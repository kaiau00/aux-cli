package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/diff"
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
)

type uiMessageType int

const (
	userMessageType uiMessageType = iota
	assistantMessageType
	toolMessageType

	maxResultHeight = 10
)

type uiMessage struct {
	ID          string
	messageType uiMessageType
	position    int
	height      int
	content     string
}

func toMarkdown(content string, focused bool, width int) string {
	r := styles.GetMarkdownRenderer(width)
	rendered, _ := r.Render(content)
	return rendered
}

func renderMessage(msg string, isUser bool, isFocused bool, width int, info ...string) string {
	t := theme.CurrentTheme()
	bg := t.Background()

	// A single-width accent bar, not a filled block: user and assistant
	// messages are told apart by BorderForeground hue alone (Info vs
	// Primary), not by weight or fill, so the framing stays light no matter
	// how long the conversation gets.
	style := styles.BaseStyle().
		Width(width - 1).
		BorderLeft(true).
		Foreground(t.TextMuted()).
		BorderForeground(t.Primary()).
		BorderStyle(lipgloss.NormalBorder())

	if isUser {
		style = style.
			BorderForeground(t.Info()).
			Foreground(t.Text())
	}

	// Apply markdown formatting and handle background color
	parts := []string{
		styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(msg, isFocused, width), bg),
	}

	// Remove newline at the end
	parts[0] = strings.TrimSuffix(parts[0], "\n")
	if len(info) > 0 {
		parts = append(parts, info...)
	}

	rendered := style.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		),
	)

	return rendered
}

func renderUserMessage(msg message.Message, isFocused bool, width int, position int) uiMessage {
	var styledAttachments []string
	t := theme.CurrentTheme()
	attachmentStyles := styles.BaseStyle().
		MarginLeft(1).
		Background(t.TextMuted()).
		Foreground(t.Text())
	for _, attachment := range msg.BinaryContent() {
		file := filepath.Base(attachment.Path)
		var filename string
		if len(file) > 10 {
			filename = fmt.Sprintf(" %s %s...", styles.DocumentIcon, file[0:7])
		} else {
			filename = fmt.Sprintf(" %s %s", styles.DocumentIcon, file)
		}
		styledAttachments = append(styledAttachments, attachmentStyles.Render(filename))
	}
	content := ""
	if len(styledAttachments) > 0 {
		attachmentContent := styles.BaseStyle().
			Width(width).
			Background(t.BackgroundSecondary()).
			Render(lipgloss.JoinHorizontal(lipgloss.Left, styledAttachments...))
		content = renderMessage(msg.Content().String(), true, isFocused, width, attachmentContent)
	} else {
		content = renderMessage(msg.Content().String(), true, isFocused, width)
	}
	userMsg := uiMessage{
		ID:          msg.ID,
		messageType: userMessageType,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
	return userMsg
}

// Returns multiple uiMessages because of the tool calls
func renderAssistantMessage(
	msg message.Message,
	msgIndex int,
	allMessages []message.Message, // we need this to get tool results and the user message
	reasoningMessages []message.Message,
	messagesService message.Service, // We need this to get the task tool messages
	focusedUIMessageId string,
	isSummary bool,
	thinkingExpanded bool,
	spinnerFrame string,
	width int,
	position int,
) []uiMessage {
	messages := []uiMessage{}
	content := msg.Content().String()
	finished := msg.IsFinished()
	finishData := msg.FinishPart()
	// Prose is shown whether or not the message also calls a tool. It used to
	// be blanked for any message carrying a tool call, which hid the agent's
	// own account of what it was doing: measured against this repository's
	// database, 94 of the 158 assistant messages that carried prose over 40
	// characters -- 59% -- never reached the screen.
	showAsFinalResponse := true
	hasReasoningDetails := hasAnyReasoningDetails(reasoningMessages)
	info := []string{}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	// Add finish info if available
	if finished {
		switch finishData.Reason {
		case message.FinishReasonEndTurn:
			took := formatSecondsDiff(msg.CreatedAt, finishData.Time)
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.NameOf(msg.Model), took)),
			)
		case message.FinishReasonCanceled:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.NameOf(msg.Model), "canceled")),
			)
		case message.FinishReasonError:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.NameOf(msg.Model), "error")),
			)
		case message.FinishReasonPermissionDenied:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.NameOf(msg.Model), "permission denied")),
			)
		}
	}
	if hasReasoningDetails {
		if thinkingExpanded {
			reasoningRendered := renderReasoningDetails(
				msg.ID,
				focusedUIMessageId,
				reasoningMessages,
				allMessages,
				messagesService,
				width,
			)
			reasoningMsg := uiMessage{
				ID:          msg.ID,
				messageType: assistantMessageType,
				position:    position,
				height:      lipgloss.Height(reasoningRendered),
				content:     reasoningRendered,
			}
			messages = append(messages, reasoningMsg)
			position += reasoningMsg.height
			position++ // for the space
		} else if msg.IsThinking() || content == "" {
			reasoningPreview := renderReasoningPreview(
				width,
				spinnerFrame,
			)
			reasoningMsg := uiMessage{
				ID:          msg.ID,
				messageType: assistantMessageType,
				position:    position,
				height:      lipgloss.Height(reasoningPreview),
				content:     reasoningPreview,
			}
			messages = append(messages, reasoningMsg)
			position += reasoningMsg.height
			position++ // for the space
		}
	}
	if content != "" || (showAsFinalResponse && finished && finishData.Reason == message.FinishReasonEndTurn) {
		if content == "" {
			content = "*Finished without output*"
		}
		if isSummary {
			info = append(info, baseStyle.Width(width-1).Foreground(t.TextMuted()).Render(" (summary)"))
		}
		if hasReasoningDetails && !thinkingExpanded && !msg.IsThinking() {
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(" Tab to show reasoning"))
		}

		content = renderMessage(content, false, true, width, info...)
		responseMsg := uiMessage{
			ID:          msg.ID,
			messageType: assistantMessageType,
			position:    position,
			height:      lipgloss.Height(content),
			content:     content,
		}
		messages = append(messages, responseMsg)
		position += responseMsg.height
		position++ // for the space
	}

	// The tool calls this message made, in order, inline. They were previously
	// reachable only by expanding the collapsed reasoning block, so a turn that
	// ran eight tools showed the user nothing but "reasoning hidden".
	for i, toolCall := range msg.ToolCalls() {
		toolMsg := renderToolMessage(
			toolCall,
			allMessages,
			messagesService,
			focusedUIMessageId,
			false,
			width,
			i+1,
			false,
		)
		toolMsg.ID = msg.ID
		toolMsg.position = position
		messages = append(messages, toolMsg)
		position += toolMsg.height
		position++ // for the space
	}
	return messages
}

// hasReasoningDetails reports whether a message carries model thinking worth
// collapsing behind Tab.
//
// It used to answer yes for prose alongside a tool call, and for any tool call
// at all, which is what put the entire working process behind a keypress. Only
// the model's own thinking is hidden now; prose and tool calls render inline.
func hasReasoningDetails(msg message.Message) bool {
	return msg.ReasoningContent().Thinking != ""
}

func hasAnyReasoningDetails(messages []message.Message) bool {
	for _, msg := range messages {
		if hasReasoningDetails(msg) {
			return true
		}
	}
	return false
}

func renderReasoningDetails(
	msgID string,
	focusedUIMessageId string,
	reasoningMessages []message.Message,
	allMessages []message.Message,
	messagesService message.Service,
	width int,
) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	bg := t.Background()

	style := baseStyle.
		Width(width - 1).
		BorderLeft(true).
		Foreground(t.TextMuted()).
		BorderForeground(t.Primary()).
		BorderStyle(lipgloss.NormalBorder())

	parts := []string{}

	for _, reasoningMsg := range reasoningMessages {
		thinkingContent := reasoningMsg.ReasoningContent().Thinking
		if thinkingContent != "" {
			parts = append(parts, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render("Thinking"))
			parts = append(parts, styles.ForceReplaceBackgroundWithLipgloss(
				toMarkdown(thinkingContent, msgID == focusedUIMessageId, width),
				bg,
			))
		}

	}
	if len(parts) == 0 {
		parts = append(parts, "Working...")
	}
	parts = append(parts, baseStyle.
		Width(width-1).
		Foreground(t.TextMuted()).
		Render(" ↑ Tab to collapse"))

	return style.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func findToolResponse(toolCallID string, futureMessages []message.Message) *message.ToolResult {
	for _, msg := range futureMessages {
		for _, result := range msg.ToolResults() {
			if result.ToolCallID == toolCallID {
				return &result
			}
		}
	}
	return nil
}

func toolName(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Task"
	case tools.BashToolName:
		return "Bash"
	case tools.EditToolName:
		return "Edit"
	case tools.FetchToolName:
		return "Fetch"
	case tools.GlobToolName:
		return "Glob"
	case tools.GrepToolName:
		return "Grep"
	case tools.LSToolName:
		return "List"
	case tools.SourcegraphToolName:
		return "Sourcegraph"
	case tools.ViewToolName:
		return "View"
	case tools.WriteToolName:
		return "Write"
	case tools.PatchToolName:
		return "Patch"
	}
	return name
}

func getToolAction(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Preparing prompt..."
	case tools.BashToolName:
		return "Building command..."
	case tools.EditToolName:
		return "Preparing edit..."
	case tools.FetchToolName:
		return "Writing fetch..."
	case tools.GlobToolName:
		return "Finding files..."
	case tools.GrepToolName:
		return "Searching content..."
	case tools.LSToolName:
		return "Listing directory..."
	case tools.SourcegraphToolName:
		return "Searching code..."
	case tools.ViewToolName:
		return "Reading file..."
	case tools.WriteToolName:
		return "Preparing write..."
	case tools.PatchToolName:
		return "Preparing patch..."
	}
	return "Working..."
}

// renders params, params[0] (params[1]=params[2] ....)
// When expanded is true the main parameter is shown on its own line and is
// not truncated, so the full input to the tool is visible.
func renderParams(paramsWidth int, expanded bool, params ...string) string {
	if len(params) == 0 {
		return ""
	}
	mainParam := params[0]
	if expanded {
		mainParam = strings.ReplaceAll(mainParam, "\n", " ")
		return mainParam
	}
	if len(mainParam) > paramsWidth {
		mainParam = mainParam[:paramsWidth-3] + "..."
	}

	if len(params) == 1 {
		return mainParam
	}
	otherParams := params[1:]
	// create pairs of key/value
	// if odd number of params, the last one is a key without value
	if len(otherParams)%2 != 0 {
		otherParams = append(otherParams, "")
	}
	parts := make([]string, 0, len(otherParams)/2)
	for i := 0; i < len(otherParams); i += 2 {
		key := otherParams[i]
		value := otherParams[i+1]
		if value == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	partsRendered := strings.Join(parts, ", ")
	remainingWidth := paramsWidth - lipgloss.Width(partsRendered) - 5 // for the space
	if remainingWidth < 30 {
		// No space for the params, just show the main
		return mainParam
	}

	if len(parts) > 0 {
		mainParam = fmt.Sprintf("%s (%s)", mainParam, strings.Join(parts, ", "))
	}

	return ansi.Truncate(mainParam, paramsWidth, "...")
}

func removeWorkingDirPrefix(path string) string {
	wd := config.WorkingDirectory()
	if strings.HasPrefix(path, wd) {
		path = strings.TrimPrefix(path, wd)
	}
	if strings.HasPrefix(path, "/") {
		path = strings.TrimPrefix(path, "/")
	}
	if strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	if strings.HasPrefix(path, "../") {
		path = strings.TrimPrefix(path, "../")
	}
	return path
}

func renderToolParams(paramWidth int, toolCall message.ToolCall, expanded bool) string {
	params := ""
	switch toolCall.Name {
	case agent.AgentToolName:
		var params agent.AgentParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		prompt := strings.ReplaceAll(params.Prompt, "\n", " ")
		return renderParams(paramWidth, expanded, prompt)
	case tools.BashToolName:
		var params tools.BashParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		command := strings.ReplaceAll(params.Command, "\n", " ")
		return renderParams(paramWidth, expanded, command)
	case tools.EditToolName:
		var params tools.EditParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		return renderParams(paramWidth, expanded, filePath)
	case tools.FetchToolName:
		var params tools.FetchParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		url := params.URL
		toolParams := []string{
			url,
		}
		if params.Format != "" {
			toolParams = append(toolParams, "format", params.Format)
		}
		if params.Timeout != 0 {
			toolParams = append(toolParams, "timeout", (time.Duration(params.Timeout) * time.Second).String())
		}
		return renderParams(paramWidth, expanded, toolParams...)
	case tools.GlobToolName:
		var params tools.GlobParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		pattern := params.Pattern
		toolParams := []string{
			pattern,
		}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		return renderParams(paramWidth, expanded, toolParams...)
	case tools.GrepToolName:
		var params tools.GrepParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		pattern := params.Pattern
		toolParams := []string{
			pattern,
		}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		if params.Include != "" {
			toolParams = append(toolParams, "include", params.Include)
		}
		if params.LiteralText {
			toolParams = append(toolParams, "literal", "true")
		}
		return renderParams(paramWidth, expanded, toolParams...)
	case tools.LSToolName:
		var params tools.LSParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		path := params.Path
		if path == "" {
			path = "."
		}
		return renderParams(paramWidth, expanded, path)
	case tools.SourcegraphToolName:
		var params tools.SourcegraphParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		return renderParams(paramWidth, expanded, params.Query)
	case tools.ViewToolName:
		var params tools.ViewParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		toolParams := []string{
			filePath,
		}
		if params.Limit != 0 {
			toolParams = append(toolParams, "limit", fmt.Sprintf("%d", params.Limit))
		}
		if params.Offset != 0 {
			toolParams = append(toolParams, "offset", fmt.Sprintf("%d", params.Offset))
		}
		return renderParams(paramWidth, expanded, toolParams...)
	case tools.WriteToolName:
		var params tools.WriteParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		return renderParams(paramWidth, expanded, filePath)
	default:
		input := strings.ReplaceAll(toolCall.Input, "\n", " ")
		params = renderParams(paramWidth, expanded, input)
	}
	return params
}

func truncateHeight(content string, height int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		return strings.Join(lines[:height], "\n")
	}
	return content
}

func renderToolResponse(toolCall message.ToolCall, response message.ToolResult, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if response.IsError {
		errContent := fmt.Sprintf("Error: %s", strings.ReplaceAll(response.Content, "\n", " "))
		errContent = ansi.Truncate(errContent, width-1, "...")
		return baseStyle.
			Width(width).
			Foreground(t.Error()).
			Render(errContent)
	}

	resultContent := truncateHeight(response.Content, maxResultHeight)
	switch toolCall.Name {
	case agent.AgentToolName:
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, false, width),
			t.Background(),
		)
	case tools.BashToolName:
		resultContent = fmt.Sprintf("```bash\n%s\n```", resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.EditToolName:
		metadata := tools.EditResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		truncDiff := truncateHeight(metadata.Diff, maxResultHeight)
		formattedDiff, _ := diff.FormatDiff(truncDiff, diff.WithTotalWidth(width))
		return formattedDiff
	case tools.FetchToolName:
		var params tools.FetchParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		mdFormat := "markdown"
		switch params.Format {
		case "text":
			mdFormat = "text"
		case "html":
			mdFormat = "html"
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", mdFormat, resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.GlobToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.GrepToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.LSToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.SourcegraphToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.ViewToolName:
		metadata := tools.ViewResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		ext := filepath.Ext(metadata.FilePath)
		if ext == "" {
			ext = ""
		} else {
			ext = strings.ToLower(ext[1:])
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", ext, truncateHeight(metadata.Content, maxResultHeight))
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.WriteToolName:
		params := tools.WriteParams{}
		json.Unmarshal([]byte(toolCall.Input), &params)
		metadata := tools.WriteResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		ext := filepath.Ext(params.FilePath)
		if ext == "" {
			ext = ""
		} else {
			ext = strings.ToLower(ext[1:])
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", ext, truncateHeight(params.Content, maxResultHeight))
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	default:
		resultContent = fmt.Sprintf("```text\n%s\n```", resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	}
}

// renderToolMessage renders a single tool call. When `expanded` is true (the
// parent assistant message is in "show reasoning" mode), tool calls are
// rendered with their full params (no truncation), and nested Task agent
// calls recursively render the full child session — including the child's
// reasoning + its own tool calls.
func renderToolMessage(
	toolCall message.ToolCall,
	allMessages []message.Message,
	messagesService message.Service,
	focusedUIMessageId string,
	nested bool,
	width int,
	position int,
	expanded bool,
) uiMessage {
	if nested {
		width = width - 3
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	style := baseStyle.
		Width(width - 1).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		PaddingLeft(1).
		BorderForeground(t.TextMuted())

	response := findToolResponse(toolCall.ID, allMessages)
	toolNameText := baseStyle.Foreground(t.TextMuted()).
		Render(fmt.Sprintf("%s: ", toolName(toolCall.Name)))

	if !toolCall.Finished {
		// Get a brief description of what the tool is doing
		toolAction := getToolAction(toolCall.Name)

		progressText := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(fmt.Sprintf("%s", toolAction))

		content := style.Render(lipgloss.JoinHorizontal(lipgloss.Left, toolNameText, progressText))
		toolMsg := uiMessage{
			messageType: toolMessageType,
			position:    position,
			height:      lipgloss.Height(content),
			content:     content,
		}
		return toolMsg
	}

	params := renderToolParams(width-2-lipgloss.Width(toolNameText), toolCall, expanded)
	responseContent := ""
	if response != nil {
		responseContent = renderToolResponse(toolCall, *response, width-2)
		responseContent = strings.TrimSuffix(responseContent, "\n")
	} else {
		responseContent = baseStyle.
			Italic(true).
			Width(width - 2).
			Foreground(t.TextMuted()).
			Render("Waiting for response...")
	}

	parts := []string{}
	if !nested {
		formattedParams := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(params)

		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Left, toolNameText, formattedParams))
	} else {
		prefix := baseStyle.
			Foreground(t.TextMuted()).
			Render(" └ ")
		formattedParams := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(params)
		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Left, prefix, toolNameText, formattedParams))
	}

	if toolCall.Name == agent.AgentToolName {
		taskMessages, _ := messagesService.List(context.Background(), toolCall.ID)
		// When expanded, render the full child session — each assistant
		// message's reasoning + tool calls — so the user can see exactly
		// what the sub-agent is doing.
		if expanded {
			childPosition := 0
			for childIndex, childMsg := range taskMessages {
				if childMsg.Role != message.Assistant {
					continue
				}
				var childReasoning []message.Message
				if hasReasoningDetails(childMsg) {
					childReasoning = []message.Message{childMsg}
				}
				childUIMsgs := renderAssistantMessage(
					childMsg,
					childIndex,
					taskMessages,
					childReasoning,
					messagesService,
					focusedUIMessageId,
					false,
					true, // child reasoning is always shown when expanded
					"",
					width,
					childPosition,
				)
				for _, u := range childUIMsgs {
					parts = append(parts, u.content)
					childPosition += u.height + 1
				}
			}
		} else {
			toolCalls := []message.ToolCall{}
			for _, v := range taskMessages {
				toolCalls = append(toolCalls, v.ToolCalls()...)
			}
			for _, call := range toolCalls {
				rendered := renderToolMessage(call, []message.Message{}, messagesService, focusedUIMessageId, true, width, 0, false)
				parts = append(parts, rendered.content)
			}
		}
	}
	if responseContent != "" && !nested {
		parts = append(parts, responseContent)
	}

	content := style.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		),
	)
	if nested {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		)
	}
	toolMsg := uiMessage{
		messageType: toolMessageType,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
	return toolMsg
}

// formatSecondsDiff renders how long a turn took, from two whole-second Unix
// timestamps.
//
// Both of its arguments are seconds: msg.CreatedAt is written by
// strftime('%s', 'now') and Finish.Time by time.Now().Unix(). The previous
// version divided by 1000 to "convert to seconds", so every duration in the
// transcript read a thousand times too small -- measured against this
// repository's own database, replies run 0-46s with a median of 2s, so a
// 46-second wait was reported as "46ms".
//
// Whole seconds in means there is no sub-second detail to report, so a
// difference under a second says so rather than inventing a millisecond
// figure.
func formatSecondsDiff(start, end int64) string {
	seconds := end - start
	if seconds < 0 {
		// A finish stamp before the creation stamp means a clock change or a
		// malformed row. "<1s" is wrong, but a negative duration is worse and
		// is what the old code printed: a fixture here produced
		// "(-1791389564ms)".
		seconds = 0
	}
	switch {
	case seconds < 1:
		return "<1s"
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	default:
		return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	}
}

// renderReasoningPreview keeps hidden agent internals discoverable without
// showing thoughts, tool details, or intermediate drafts in the main
// transcript. A single line: the spinner and "reasoning hidden" already read
// as one unit, so the expand hint joins them instead of taking its own row.
func renderReasoningPreview(width int, spinnerFrame string) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	style := baseStyle.
		Width(width - 1).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Primary()).
		PaddingLeft(1)

	spinnerStyle := baseStyle.Foreground(t.Primary()).Bold(true)
	labelStyle := baseStyle.Foreground(t.TextMuted()).Italic(true)

	return style.Render(lipgloss.JoinHorizontal(
		lipgloss.Left,
		spinnerStyle.Render(spinnerFrame),
		labelStyle.Render(" reasoning hidden · "),
		labelStyle.Render("↓ Tab to expand"),
	))
}
