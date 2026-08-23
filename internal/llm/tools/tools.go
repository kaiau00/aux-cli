package tools

import (
	"context"
	"encoding/json"
	"os"

	"github.com/kaiau00/aux-cli/internal/config"
)

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
}

type toolResponseType string

type (
	sessionIDContextKey       string
	messageIDContextKey       string
	turnIDContextKey          string
	modelCallIDContextKey     string
	taskIDContextKey          string
	parentTaskIDContextKey    string
	projectIDContextKey       string
	toolExecutionIDContextKey string
	workingDirContextKey      string
)

const (
	ToolResponseTypeText  toolResponseType = "text"
	ToolResponseTypeImage toolResponseType = "image"

	SessionIDContextKey   sessionIDContextKey   = "session_id"
	MessageIDContextKey   messageIDContextKey   = "message_id"
	TurnIDContextKey      turnIDContextKey      = "turn_id"
	ModelCallIDContextKey modelCallIDContextKey = "model_call_id"
	TaskIDContextKey      taskIDContextKey      = "task_id"
	// ParentTaskIDContextKey carries the task id a subagent's own task should be
	// linked under. Set by the agent tool before spawning
	// a subagent; read by task.Coordinator.Begin when present.
	ParentTaskIDContextKey    parentTaskIDContextKey    = "parent_task_id"
	ProjectIDContextKey       projectIDContextKey       = "project_id"
	ToolExecutionIDContextKey toolExecutionIDContextKey = "tool_execution_id"
	// WorkingDirContextKey overrides the directory filesystem tools operate in
	// for this call, e.g. a subagent's isolated worktree. Set by the agent tool before spawning a subagent; read via
	// ResolveWorkingDir. Absent for ordinary top-level tool calls, which keep
	// using the process-wide configured working directory.
	WorkingDirContextKey workingDirContextKey = "working_dir"
)

type ToolResponse struct {
	Type     toolResponseType `json:"type"`
	Content  string           `json:"content"`
	Metadata string           `json:"metadata,omitempty"`
	IsError  bool             `json:"is_error"`
}

func NewTextResponse(content string) ToolResponse {
	return ToolResponse{
		Type:    ToolResponseTypeText,
		Content: content,
	}
}

func WithResponseMetadata(response ToolResponse, metadata any) ToolResponse {
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return response
		}
		response.Metadata = string(metadataBytes)
	}
	return response
}

func NewTextErrorResponse(content string) ToolResponse {
	return ToolResponse{
		Type:    ToolResponseTypeText,
		Content: content,
		IsError: true,
	}
}

type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

type BaseTool interface {
	Info() ToolInfo
	Run(ctx context.Context, params ToolCall) (ToolResponse, error)
}

// ResolveWorkingDir returns the directory a filesystem-facing tool call
// should operate in: a per-call override set on ctx if present (e.g. a
// subagent's isolated worktree), otherwise the process-wide configured
// working directory.
//
// It never panics. config.WorkingDirectory panics when no config has been
// loaded, which is legitimate in tests and in early startup paths, so that
// case falls back to the process working directory — every caller here is
// resolving a path, and a panic deep inside a tool call is never the right
// answer.
func ResolveWorkingDir(ctx context.Context) (dir string) {
	if v, ok := ctx.Value(WorkingDirContextKey).(string); ok && v != "" {
		return v
	}
	defer func() {
		if recover() != nil {
			if cwd, err := os.Getwd(); err == nil {
				dir = cwd
			}
		}
	}()
	return config.WorkingDirectory()
}

func GetContextValues(ctx context.Context) (string, string) {
	sessionID := ctx.Value(SessionIDContextKey)
	messageID := ctx.Value(MessageIDContextKey)
	if sessionID == nil {
		return "", ""
	}
	if messageID == nil {
		return sessionID.(string), ""
	}
	return sessionID.(string), messageID.(string)
}

// Correlation carries the runtime identifiers that link a tool execution back to
// its session, message, turn, model call, task, and project. It replaces
// reaching into many individual context keys.
type Correlation struct {
	SessionID   string
	MessageID   string
	TurnID      string
	ModelCallID string
	TaskID      string
	ProjectID   string
}

func ctxString(ctx context.Context, key any) string {
	if v, ok := ctx.Value(key).(string); ok {
		return v
	}
	return ""
}

// CorrelationFromContext extracts the runtime correlation identifiers from ctx.
// Missing values are returned as empty strings.
func CorrelationFromContext(ctx context.Context) Correlation {
	return Correlation{
		SessionID:   ctxString(ctx, SessionIDContextKey),
		MessageID:   ctxString(ctx, MessageIDContextKey),
		TurnID:      ctxString(ctx, TurnIDContextKey),
		ModelCallID: ctxString(ctx, ModelCallIDContextKey),
		TaskID:      ctxString(ctx, TaskIDContextKey),
		ProjectID:   ctxString(ctx, ProjectIDContextKey),
	}
}
