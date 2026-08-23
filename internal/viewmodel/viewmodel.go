package viewmodel

// TaskHeaderVM is the compact task header: project, task,
// stage, model, context, cost.
type TaskHeaderVM struct {
	Project      string         `json:"project"`
	Branch       string         `json:"branch,omitempty"`
	TaskID       string         `json:"taskId"`
	Objective    string         `json:"objective"`
	Mode         string         `json:"mode,omitempty"`
	Stage        string         `json:"stage"`
	State        ComponentState `json:"state"`
	Model        string         `json:"model,omitempty"`
	ContextUsed  int64          `json:"contextUsed"`
	ContextLimit int64          `json:"contextLimit"`
	Cost         float64        `json:"cost"`
	CostUnknown  bool           `json:"costUnknown,omitempty"`
}

// TaskSummaryVM is a lightweight task list item for the dashboard's active-work
// navigation. It projects durable task state.
type TaskSummaryVM struct {
	TaskID    string         `json:"taskId"`
	Objective string         `json:"objective"`
	Mode      string         `json:"mode,omitempty"`
	State     ComponentState `json:"state"`
	Outcome   string         `json:"outcome,omitempty"`
	SessionID string         `json:"sessionId,omitempty"`
	CreatedAt int64          `json:"createdAt"`
}

// ActivityGroupVM is a collapsed activity stage.
type ActivityGroupVM struct {
	Kind       ActivityKind   `json:"kind"`
	State      ComponentState `json:"state"`
	Count      int            `json:"count"`
	Errors     int            `json:"errors"`
	Summary    string         `json:"summary,omitempty"`
	DurationMS int64          `json:"durationMs,omitempty"`
}

// ChangeSummaryVM is the persistent changed-files surface.
type ChangeSummaryVM struct {
	Files    []ChangedFileVM `json:"files"`
	Added    int             `json:"added"`
	Modified int             `json:"modified"`
	Deleted  int             `json:"deleted"`
}

// ChangedFileVM is one changed file.
type ChangedFileVM struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

// ValidationSummaryVM is the acceptance/validation surface.
// It distinguishes validated from merely-claimed and never implies success
// without evidence.
type ValidationSummaryVM struct {
	Criteria  []CriterionVM  `json:"criteria"`
	Validated int            `json:"validated"`
	Blocked   int            `json:"blocked"`
	Uncovered int            `json:"uncovered"`
	Overall   ComponentState `json:"overall"`
}

// CriterionVM is one acceptance criterion's display state.
type CriterionVM struct {
	Description string         `json:"description"`
	State       ComponentState `json:"state"`
}

// ContextBudgetVM is the signature context composition.
type ContextBudgetVM struct {
	TotalTokens    int64               `json:"totalTokens"`
	LimitTokens    int64               `json:"limitTokens"`
	Categories     []ContextCategoryVM `json:"categories"`
	ResidentPages  int                 `json:"residentPages"`
	AvailablePages int                 `json:"availablePages"`
	PinnedPages    int                 `json:"pinnedPages"`
	SavedTokens    int64               `json:"savedTokens,omitempty"`
}

// ContextCategoryVM is one budget category row.
type ContextCategoryVM struct {
	Label  string `json:"label"`
	Tokens int64  `json:"tokens"`
}
