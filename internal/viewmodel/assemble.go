package viewmodel

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
)

// TaskView is the assembled, read-only view of a task for the dashboard/TUI
// (roadmapplan.md §18). Every field traces to durable runtime data.
type TaskView struct {
	Header     TaskHeaderVM        `json:"header"`
	Activity   []ActivityGroupVM   `json:"activity"`
	Validation ValidationSummaryVM `json:"validation"`
	Context    ContextBudgetVM     `json:"context"`
	Changes    ChangeSummaryVM     `json:"changes"`
}

// Stores bundles the read dependencies needed to assemble a TaskView.
type Stores struct {
	Tasks       *task.Store
	Events      eventstore.Service
	Validations *validation.Service
	Checkpoints *checkpoint.Store
	Pages       *contextstore.Store
	Ledger      cost.Service
}

// RecentTasks returns lightweight summaries of the most recent tasks for the
// dashboard's active-work navigation (roadmapplan.md §13.12).
func (s Stores) RecentTasks(ctx context.Context, limit int) ([]TaskSummaryVM, error) {
	tasks, err := s.Tasks.ListRecent(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]TaskSummaryVM, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, BuildTaskSummary(t))
	}
	return out, nil
}

// TaskView assembles the full read model for a task from durable state.
func (s Stores) TaskView(ctx context.Context, taskID string) (TaskView, error) {
	t, err := s.Tasks.GetTask(ctx, taskID)
	if err != nil {
		return TaskView{}, err
	}
	events, err := s.Events.List(ctx, eventstore.Filter{TaskID: taskID})
	if err != nil {
		return TaskView{}, err
	}
	totals, err := s.Ledger.TaskTotals(ctx, taskID)
	if err != nil {
		return TaskView{}, err
	}

	budget := cost.DefaultBudget(cost.ModeBalanced)
	view := TaskView{
		Header: TaskHeaderVM{
			TaskID:       t.ID,
			Objective:    t.Objective,
			Mode:         string(t.Mode),
			Stage:        StageFromEvents(events),
			State:        StateForTaskStatus(t.Status),
			ContextUsed:  totals.PromptTokens,
			ContextLimit: budget.MaxInputTokens,
			Cost:         totals.Cost,
			CostUnknown:  totals.CostUnknown,
		},
		Activity: BuildActivityGroups(events),
	}

	// Validation: join the task spec's acceptance criteria with proof-of-done.
	if spec, ok, serr := s.Tasks.LatestSpec(ctx, taskID); serr == nil && ok && s.Validations != nil && len(spec.AcceptanceCriteria) > 0 {
		ids := make([]string, 0, len(spec.AcceptanceCriteria))
		descByID := map[string]string{}
		for _, c := range spec.AcceptanceCriteria {
			ids = append(ids, c.ID)
			descByID[c.ID] = c.Description
		}
		states, verr := s.Validations.ProofOfDone(ctx, taskID, ids)
		if verr == nil {
			inputs := make([]CriterionInput, 0, len(ids))
			for _, id := range ids {
				inputs = append(inputs, CriterionInput{Description: descByID[id], State: states[id]})
			}
			view.Validation = BuildValidationSummary(inputs)
		}
	} else {
		view.Validation = BuildValidationSummary(nil)
	}

	// Context: bindings of the task's most recent model call, plus saved tokens
	// from the latest context.compiled event.
	if s.Pages != nil {
		if calls, cerr := s.Ledger.ListCallsByTask(ctx, taskID); cerr == nil && len(calls) > 0 {
			last := calls[len(calls)-1]
			if bindings, berr := s.Pages.BindingsForCall(ctx, last.ID); berr == nil {
				view.Context = BuildContextBudget(bindings, budget.MaxInputTokens, savedTokensFromEvents(events))
			}
		}
	}

	// Changes: entries of the task's latest checkpoint (empty until checkpoints
	// are created for the task).
	if s.Checkpoints != nil {
		if cps, cerr := s.Checkpoints.ListByTask(ctx, taskID); cerr == nil && len(cps) > 0 {
			if entries, eerr := s.Checkpoints.Entries(ctx, cps[len(cps)-1].ID); eerr == nil {
				view.Changes = BuildChangeSummary(entries)
			}
		}
	}

	return view, nil
}

func savedTokensFromEvents(events []eventstore.Event) int64 {
	var saved int64
	for _, e := range events {
		if e.Type == eventstore.ContextCompiled {
			var cp eventstore.ContextPayload
			if e.DecodePayload(&cp) == nil {
				saved = cp.SavedTokens
			}
		}
	}
	return saved
}
