package eventstore

// Typed payloads for the events emitted so far. Each event type has a small,
// stable payload struct so consumers never parse free-form JSON.

// ModelCallPayload accompanies model_call.* events.
type ModelCallPayload struct {
	ModelCallID         string  `json:"modelCallId"`
	Provider            string  `json:"provider,omitempty"`
	Model               string  `json:"model,omitempty"`
	Status              string  `json:"status,omitempty"`
	InputTokens         int64   `json:"inputTokens,omitempty"`
	OutputTokens        int64   `json:"outputTokens,omitempty"`
	CacheCreationTokens int64   `json:"cacheCreationTokens,omitempty"`
	CacheReadTokens     int64   `json:"cacheReadTokens,omitempty"`
	EstimatedCost       float64 `json:"estimatedCost,omitempty"`
	CostState           string  `json:"costState,omitempty"`
	LatencyMS           int64   `json:"latencyMs,omitempty"`
	TTFTMS              int64   `json:"ttftMs,omitempty"`
	ErrorCode           string  `json:"errorCode,omitempty"`
}

// TurnPayload accompanies turn.* events.
type TurnPayload struct {
	TurnID       string `json:"turnId"`
	MessageID    string `json:"messageId,omitempty"`
	ToolCalls    int    `json:"toolCalls,omitempty"`
	FinishReason string `json:"finishReason,omitempty"`
}

// ContextPayload accompanies context.* events (what the prompt compiler produced).
type ContextPayload struct {
	CallID         string `json:"callId,omitempty"`
	MessageCount   int    `json:"messageCount,omitempty"`
	ToolCount      int    `json:"toolCount,omitempty"`
	TokenEstimate  int64  `json:"tokenEstimate,omitempty"`
	StablePrefixID string `json:"stablePrefixId,omitempty"`
	ResidentPages  int    `json:"residentPages,omitempty"`
	AvailablePages int    `json:"availablePages,omitempty"`
	SavedTokens    int64  `json:"savedTokens,omitempty"`
}

// TaskPayload accompanies task.* events.
type TaskPayload struct {
	TaskID           string `json:"taskId"`
	Objective        string `json:"objective,omitempty"`
	Mode             string `json:"mode,omitempty"`
	Status           string `json:"status,omitempty"`
	Outcome          string `json:"outcome,omitempty"`
	ProfileVersionID string `json:"profileVersionId,omitempty"`
}

// CheckpointPayload accompanies checkpoint.* events.
type CheckpointPayload struct {
	CheckpointID string `json:"checkpointId"`
	ParentID     string `json:"parentId,omitempty"`
	Label        string `json:"label,omitempty"`
	Entries      int    `json:"entries,omitempty"`
}

// SkillPayload accompanies skill.* events.
type SkillPayload struct {
	SkillID   string `json:"skillId"`
	Name      string `json:"name,omitempty"`
	State     string `json:"state,omitempty"`
	VersionID string `json:"versionId,omitempty"`
}

// BudgetPayload accompanies budget.* and governor.decision events.
type BudgetPayload struct {
	Pressure  float64 `json:"pressure,omitempty"`
	Exhausted bool    `json:"exhausted,omitempty"`
	Action    string  `json:"action,omitempty"`
	Reason    string  `json:"reason,omitempty"`
}

// PolicyPayload accompanies policy.* (governor policy lifecycle) events.
type PolicyPayload struct {
	PolicyID string `json:"policyId"`
	State    string `json:"state,omitempty"`
}

// ValidationPayload accompanies validation.* events.
type ValidationPayload struct {
	ValidationRunID string `json:"validationRunId"`
	Command         string `json:"command,omitempty"`
	Status          string `json:"status,omitempty"`
	ExitCode        int    `json:"exitCode,omitempty"`
	DurationMS      int64  `json:"durationMs,omitempty"`
	// Reason says why validation.skipped did not run anything.
	Reason string `json:"reason,omitempty"`
}

// MemoryPayload accompanies memory.* events.
type MemoryPayload struct {
	MemoryID   string  `json:"memoryId"`
	MemoryType string  `json:"memoryType,omitempty"`
	State      string  `json:"state,omitempty"`
	StableKey  string  `json:"stableKey,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// ToolPayload accompanies tool.* events.
type ToolPayload struct {
	ToolExecutionID string `json:"toolExecutionId"`
	ToolCallID      string `json:"toolCallId,omitempty"`
	ToolName        string `json:"toolName"`
	InputHash       string `json:"inputHash,omitempty"`
	Status          string `json:"status,omitempty"`
	LatencyMS       int64  `json:"latencyMs,omitempty"`
	ResponseBytes   int64  `json:"responseBytes,omitempty"`
	SavedBytes      int64  `json:"savedBytes,omitempty"`
	IsError         bool   `json:"isError,omitempty"`
	ArtifactID      string `json:"artifactId,omitempty"`
}
