package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func TestTaskViewEndpoint(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	// Seed a task and a couple of events.
	taskStore := task.NewStore(conn)
	if err := taskStore.CreateTask(ctx, task.Task{
		ID: "task-1", SessionID: "s", Objective: "add feature", Mode: task.ModeImplementation,
		Status: task.StatusRunning, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	events := eventstore.NewService(conn)
	_, _ = events.Append(ctx, eventstore.Append{Type: eventstore.TaskStarted, TaskID: "task-1"})

	stores := viewmodel.Stores{
		Tasks:       taskStore,
		Events:      events,
		Validations: validation.NewService(validation.NewStore(conn), nil),
		Checkpoints: nil, // exercised elsewhere; nil path is tolerated below
		Pages:       nil,
		Ledger:      cost.NewService(conn),
	}
	server := &Server{token: "secret", services: Services{Tasks: stores}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task-1?token=secret", nil)
	req.SetPathValue("id", "task-1")
	rec := httptest.NewRecorder()
	server.handleTaskView(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var view viewmodel.TaskView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view.Header.TaskID != "task-1" {
		t.Fatalf("header task id = %q", view.Header.TaskID)
	}
	if view.Header.State != viewmodel.StateActive {
		t.Fatalf("running task should be active, got %q", view.Header.State)
	}
	if view.Header.Objective != "add feature" {
		t.Fatalf("objective not carried: %q", view.Header.Objective)
	}
}

func TestTaskViewRequiresToken(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/x", nil)
	rec := httptest.NewRecorder()
	server.handleTaskView(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestTasksListEndpoint(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	taskStore := task.NewStore(conn)
	// Two tasks; the newer one must sort first.
	if err := taskStore.CreateTask(ctx, task.Task{
		ID: "task-old", SessionID: "s", Objective: "older", Mode: task.ModeImplementation,
		Status: task.StatusCompleted, CreatedAt: 100,
	}); err != nil {
		t.Fatalf("CreateTask old: %v", err)
	}
	if err := taskStore.CreateTask(ctx, task.Task{
		ID: "task-new", SessionID: "s", Objective: "newer", Mode: task.ModeImplementation,
		Status: task.StatusRunning, CreatedAt: 200,
	}); err != nil {
		t.Fatalf("CreateTask new: %v", err)
	}

	stores := viewmodel.Stores{Tasks: taskStore, Events: eventstore.NewService(conn), Ledger: cost.NewService(conn)}
	server := &Server{token: "secret", services: Services{Tasks: stores}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleTasksList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var list []viewmodel.TaskSummaryVM
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(list))
	}
	if list[0].TaskID != "task-new" {
		t.Fatalf("newest task should sort first, got %q", list[0].TaskID)
	}
	if list[0].State != viewmodel.StateActive {
		t.Fatalf("running task summary should be active, got %q", list[0].State)
	}
}

func TestTasksListRequiresToken(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	rec := httptest.NewRecorder()
	server.handleTasksList(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
