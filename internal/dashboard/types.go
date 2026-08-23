package dashboard

import (
	"context"
	"time"

	"github.com/kaiau00/aux-cli/internal/history"
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

// TaskReader assembles task read-only view models from durable runtime state.
// Optional; when nil the /api/v1 task endpoints are
// disabled.
type TaskReader interface {
	TaskView(ctx context.Context, taskID string) (viewmodel.TaskView, error)
	RecentTasks(ctx context.Context, limit int) ([]viewmodel.TaskSummaryVM, error)
}

// ProjectReader assembles the Project Brain view: project identity, effective profile, and related-project graph.
// Optional; when nil the /api/v1/project endpoint is disabled.
type ProjectReader interface {
	ProjectBrainView(ctx context.Context, workdir string) (viewmodel.ProjectBrainVM, error)
	ResolveProjectID(ctx context.Context, workdir string) (string, error)
}

// MemoryReader assembles the Memory & skills view. Optional; when nil the /api/v1/memory endpoint is disabled.
type MemoryReader interface {
	MemoryBrainView(ctx context.Context, projectID string) (viewmodel.MemoryBrainVM, error)
}

// ImpactReader assembles the Impact graph view.
// Optional; when nil the /api/v1/impact endpoint is disabled.
type ImpactReader interface {
	ImpactGraphView(ctx context.Context, projectID string) (viewmodel.ImpactGraphVM, error)
}

// OptimizationReader assembles the Optimization view. Optional; when nil the /api/v1/optimization endpoint is disabled.
type OptimizationReader interface {
	OptimizationView(ctx context.Context, projectID string) (viewmodel.OptimizationVM, error)
}

type RedactionMode string

const (
	RedactionRedacted RedactionMode = "redacted"
	RedactionFull     RedactionMode = "full"
	RedactionMetadata RedactionMode = "metadata"
)

type Options struct {
	Enabled       bool
	Host          string
	Port          int
	Redaction     RedactionMode
	FullContent   bool
	DataDirectory string
}

type Services struct {
	Sessions     session.Service
	Messages     message.Service
	History      history.Service
	Agent        agent.Service
	Tasks        TaskReader
	Project      ProjectReader
	Memory       MemoryReader
	Impact       ImpactReader
	Optimization OptimizationReader
	// Workdir is the working directory the Project/Memory/Impact/Optimization
	// views resolve their current project from.
	Workdir string
}

type DashboardEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
	Time int64  `json:"time"`
}

type Snapshot struct {
	Sessions []SessionDTO `json:"sessions"`
	Logs     []LogDTO     `json:"logs"`
	Stats    StatsDTO     `json:"stats"`
	Mode     string       `json:"mode"`
}

type SessionDTO struct {
	ID               string  `json:"id"`
	ParentSessionID  string  `json:"parentSessionId,omitempty"`
	Title            string  `json:"title"`
	MessageCount     int64   `json:"messageCount"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	Cost             float64 `json:"cost"`
	CreatedAt        int64   `json:"createdAt"`
	UpdatedAt        int64   `json:"updatedAt"`
}

type MessageDTO struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Role      string          `json:"role"`
	Model     string          `json:"model,omitempty"`
	Text      string          `json:"text,omitempty"`
	Reasoning string          `json:"reasoning,omitempty"`
	Tools     []ToolCallDTO   `json:"tools,omitempty"`
	Results   []ToolResultDTO `json:"results,omitempty"`
	Finish    string          `json:"finish,omitempty"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

type ToolCallDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Input    string `json:"input,omitempty"`
	Finished bool   `json:"finished"`
}

type ToolResultDTO struct {
	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name,omitempty"`
	Content    string `json:"content,omitempty"`
	Metadata   string `json:"metadata,omitempty"`
	IsError    bool   `json:"isError"`
}

type FileDTO struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Content   string `json:"content,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type AgentDTO struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId,omitempty"`
	Progress  string `json:"progress,omitempty"`
	Done      bool   `json:"done"`
	Error     string `json:"error,omitempty"`
}

type LogDTO struct {
	ID      string `json:"id"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Time    int64  `json:"time"`
	Persist bool   `json:"persist"`
}

type StatsDTO struct {
	SessionCount     int     `json:"sessionCount"`
	MessageCount     int64   `json:"messageCount"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	Cost             float64 `json:"cost"`
}

func eventType[T any](prefix string, event pubsub.Event[T]) string {
	return prefix + "." + string(event.Type)
}

func nowUnix() int64 {
	return time.Now().Unix()
}

func sessionDTO(s session.Session) SessionDTO {
	return SessionDTO{
		ID:               s.ID,
		ParentSessionID:  s.ParentSessionID,
		Title:            s.Title,
		MessageCount:     s.MessageCount,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		Cost:             s.Cost,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func logDTO(l logging.LogMessage) LogDTO {
	return LogDTO{
		ID:      l.ID,
		Level:   l.Level,
		Message: l.Message,
		Time:    l.Time.Unix(),
		Persist: l.Persist,
	}
}
