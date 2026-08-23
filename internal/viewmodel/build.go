package viewmodel

import (
	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
)

// StateForTaskStatus maps a task status to a display state.
func StateForTaskStatus(status task.Status) ComponentState {
	switch status {
	case task.StatusCreated, task.StatusCompiled:
		return StateQueued
	case task.StatusRunning:
		return StateActive
	case task.StatusCompleted:
		return StateCompleted
	case task.StatusFailed:
		return StateFailed
	case task.StatusCancelled:
		return StateCancelled
	default:
		return StateQueued
	}
}

// BuildTaskSummary projects a task into a lightweight list item.
func BuildTaskSummary(t task.Task) TaskSummaryVM {
	return TaskSummaryVM{
		TaskID:    t.ID,
		Objective: t.Objective,
		Mode:      string(t.Mode),
		State:     StateForTaskStatus(t.Status),
		Outcome:   t.Outcome,
		SessionID: t.SessionID,
		CreatedAt: t.CreatedAt,
	}
}

// StageFromEvents returns the current activity stage from the most recent
// activity-bearing event, so the header shows what Aux is doing now.
func StageFromEvents(events []eventstore.Event) string {
	stage := "idle"
	for _, e := range events {
		switch e.Type {
		case eventstore.TaskCompiled:
			stage = "planning"
		case eventstore.ModelCallStarted:
			stage = "thinking"
		case eventstore.ToolStarted:
			var tp eventstore.ToolPayload
			if e.DecodePayload(&tp) == nil {
				stage = string(activityForTool(tp.ToolName))
			}
		case eventstore.ValidationStarted:
			stage = "validating"
		case eventstore.TaskCompleted:
			stage = "completed"
		case eventstore.TaskFailed:
			stage = "failed"
		}
	}
	return stage
}

// BuildActivityGroups projects tool events into user-understandable activity
// groups, preserving error counts.
func BuildActivityGroups(events []eventstore.Event) []ActivityGroupVM {
	order := []ActivityKind{}
	groups := map[ActivityKind]*ActivityGroupVM{}
	for _, e := range events {
		if e.Type != eventstore.ToolCompleted && e.Type != eventstore.ToolFailed {
			continue
		}
		var tp eventstore.ToolPayload
		if e.DecodePayload(&tp) != nil {
			continue
		}
		kind := activityForTool(tp.ToolName)
		g, ok := groups[kind]
		if !ok {
			g = &ActivityGroupVM{Kind: kind, State: StateCompleted}
			groups[kind] = g
			order = append(order, kind)
		}
		g.Count++
		g.DurationMS += tp.LatencyMS
		if e.Type == eventstore.ToolFailed || tp.IsError {
			g.Errors++
			g.State = StateFailed
		}
	}
	out := make([]ActivityGroupVM, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

// CriterionInput pairs an acceptance-criterion description with its proof-of-done
// state (from validation.ProofOfDone joined with the task spec).
type CriterionInput struct {
	Description string
	State       validation.CriterionState
}

// BuildValidationSummary projects acceptance criteria into a display summary that
// never implies success without evidence.
func BuildValidationSummary(criteria []CriterionInput) ValidationSummaryVM {
	vm := ValidationSummaryVM{}
	anyBlocked, anyUnverified := false, false
	for _, c := range criteria {
		state := stateForCriterion(c.State)
		vm.Criteria = append(vm.Criteria, CriterionVM{Description: c.Description, State: state})
		switch state {
		case StateValidated:
			vm.Validated++
		case StateBlocked:
			vm.Blocked++
			anyBlocked = true
		case StateUnverified:
			vm.Uncovered++
			anyUnverified = true
		}
	}
	switch {
	case len(criteria) == 0:
		vm.Overall = StateUnverified
	case anyBlocked:
		vm.Overall = StateBlocked
	case anyUnverified:
		vm.Overall = StateUnverified
	default:
		vm.Overall = StateValidated
	}
	return vm
}

func stateForCriterion(s validation.CriterionState) ComponentState {
	switch s {
	case validation.Validated:
		return StateValidated
	case validation.Blocked:
		return StateBlocked
	case validation.WaivedByUser:
		return StateCompleted
	default: // uncovered, claimed, partially_evidenced
		return StateUnverified
	}
}

// BuildChangeSummary projects checkpoint entries into the changed-files surface.
func BuildChangeSummary(entries []checkpoint.Entry) ChangeSummaryVM {
	vm := ChangeSummaryVM{}
	for _, e := range entries {
		vm.Files = append(vm.Files, ChangedFileVM{Path: e.Path, Operation: string(e.Operation)})
		switch e.Operation {
		case checkpoint.OpAdd:
			vm.Added++
		case checkpoint.OpDelete:
			vm.Deleted++
		default:
			vm.Modified++
		}
	}
	return vm
}

// pageCategoryLabel maps a page type to a budget category.
func pageCategoryLabel(pageType string) string {
	switch pageType {
	case contextstore.KindTaskSpec, contextstore.KindPlanState:
		return "Task and plan"
	case contextstore.KindProjectManifest, contextstore.KindInstruction:
		return "Project knowledge"
	case contextstore.KindFileRegion, contextstore.KindSymbolDefinition, contextstore.KindSymbolReferences, contextstore.KindDependencyNeighbor:
		return "Active code"
	case contextstore.KindToolDigest, contextstore.KindValidationResult, contextstore.KindDiffSummary:
		return "Tool results"
	case contextstore.KindMemoryFact, contextstore.KindMemoryProcedure, contextstore.KindMemoryEpisode, contextstore.KindSkillManifest, contextstore.KindSkillBody:
		return "Memory and skills"
	case contextstore.KindRecentConversation:
		return "Recent conversation"
	default:
		return "Other"
	}
}

// ContextPageEntryVM is one page binding for the TUI's expanded context view.
type ContextPageEntryVM struct {
	PageType  string `json:"pageType"`
	StableKey string `json:"stableKey"`
	State     string `json:"state"`
	Tokens    int64  `json:"tokens"`
	Reason    string `json:"reason,omitempty"`
}

// ContextPageListVM groups a call's page bindings by state, for the expanded
// context view that the compact budget summarizes.
// Grouped by the states that actually exist in contextstore — resident,
// available, pinned, evicted, faulted — not aspirational states with no
// backing model.
type ContextPageListVM struct {
	Resident  []ContextPageEntryVM `json:"resident"`
	Available []ContextPageEntryVM `json:"available"`
	Pinned    []ContextPageEntryVM `json:"pinned"`
	Evicted   []ContextPageEntryVM `json:"evicted"`
	Faulted   []ContextPageEntryVM `json:"faulted"`
}

// BuildContextPageList projects per-call page bindings into the expanded,
// per-page view grouped by binding state.
func BuildContextPageList(bindings []contextstore.BoundPage) ContextPageListVM {
	var vm ContextPageListVM
	for _, b := range bindings {
		entry := ContextPageEntryVM{PageType: b.PageType, StableKey: b.StableKey, State: b.State, Tokens: b.TokenCount, Reason: b.Reason}
		switch b.State {
		case contextstore.StateResident:
			vm.Resident = append(vm.Resident, entry)
		case contextstore.StateAvailable:
			vm.Available = append(vm.Available, entry)
		case contextstore.StatePinned:
			vm.Pinned = append(vm.Pinned, entry)
		case contextstore.StateEvicted:
			vm.Evicted = append(vm.Evicted, entry)
		case contextstore.StateFaulted:
			vm.Faulted = append(vm.Faulted, entry)
		}
	}
	return vm
}

// BuildContextBudget projects per-call page bindings into a budget composition.
// Only resident pages count toward the used total.
func BuildContextBudget(bindings []contextstore.BoundPage, limitTokens, savedTokens int64) ContextBudgetVM {
	vm := ContextBudgetVM{LimitTokens: limitTokens, SavedTokens: savedTokens}
	catTokens := map[string]int64{}
	catOrder := []string{}
	for _, b := range bindings {
		switch b.State {
		case contextstore.StateResident:
			vm.ResidentPages++
			vm.TotalTokens += b.TokenCount
			label := pageCategoryLabel(b.PageType)
			if _, ok := catTokens[label]; !ok {
				catOrder = append(catOrder, label)
			}
			catTokens[label] += b.TokenCount
		case contextstore.StateAvailable:
			vm.AvailablePages++
		case contextstore.StatePinned:
			vm.PinnedPages++
			vm.ResidentPages++
			vm.TotalTokens += b.TokenCount
		}
	}
	for _, label := range catOrder {
		vm.Categories = append(vm.Categories, ContextCategoryVM{Label: label, Tokens: catTokens[label]})
	}
	return vm
}
