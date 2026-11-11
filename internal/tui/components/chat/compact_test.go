package chat

import (
	"context"
	"fmt"
	"testing"

	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/task"
)

func TestCompactionPagesRankAndPersistControls(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	q := db.New(conn)
	sess, err := session.NewService(q).Create(ctx, "pressure")
	if err != nil {
		t.Fatal(err)
	}
	tasks := task.NewStore(conn)
	if err = tasks.CreateTask(ctx, task.Task{ID: "task", SessionID: sess.ID, Mode: task.ModeImplementation, Status: task.StatusRunning, CreatedAt: 2}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.CreateTask(ctx, task.Task{ID: "old-task", SessionID: sess.ID, Mode: task.ModeImplementation, Status: task.StatusRunning, CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	ledger := cost.NewService(conn)
	if _, err = ledger.StartCall(ctx, cost.ModelCall{ID: "call", TaskID: "task", SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	pages := contextstore.NewStore(conn)
	messages := message.NewService(q)
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("tool-%d", i)
		msg, err := messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: id, Name: "view", Content: "content"}}})
		if err != nil {
			t.Fatal(err)
		}
		page, err := pages.UpsertPage(ctx, "", contextstore.KindToolDigest, "msg:"+msg.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		version, err := pages.UpsertVersion(ctx, page.ID, id, "", int64(i*100))
		if err != nil {
			t.Fatal(err)
		}
		state := contextstore.StateResident
		if i == 8 {
			state = contextstore.StateAvailable
		}
		if err = pages.Bind(ctx, contextstore.Binding{ID: id, TaskID: "task", ModelCallID: "call", PageVersionID: version.ID, State: state, TokenCount: int64(i * 100)}); err != nil {
			t.Fatal(err)
		}
	}
	m := NewContextPaneCmp(&app.App{Tasks: tasks, Cost: ledger, Pages: pages, Messages: messages})
	top, err := m.CompactionPages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if top[0].Name != "view result" {
		t.Fatalf("unnamed page: %+v", top[0])
	}
	if len(top) != 5 || top[0].Tokens != 700 || top[4].Tokens != 300 || top[0].ToolCallIDs[0] != "tool-7" {
		t.Fatalf("ranking: %+v", top)
	}
	if err = m.ApplyCompactionControl(top[0], false); err != nil {
		t.Fatal(err)
	}
	if err = m.ApplyCompactionControl(top[0], true); err != nil {
		t.Fatal(err)
	}
	top, err = m.CompactionPages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !top[0].Excluded || !top[0].Pinned {
		t.Fatalf("overrides not persisted: %+v", top[0])
	}
	if err = m.ApplyCompactionControl(top[0], true); err != nil {
		t.Fatal(err)
	}
	pins, err := pages.Pins(ctx, "task")
	if err != nil || pins["tool-7"] {
		t.Fatalf("unpin: %v %v", pins, err)
	}
	// A missing message must keep the ranking available with its stable identity.
	missing, err := pages.UpsertPage(ctx, "", contextstore.KindToolDigest, "msg:missing", "")
	if err != nil {
		t.Fatal(err)
	}
	version, err := pages.UpsertVersion(ctx, missing.ID, "missing", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := pages.Bind(ctx, contextstore.Binding{ID: "missing", TaskID: "task", ModelCallID: "call", PageVersionID: version.ID, State: contextstore.StateResident, TokenCount: 1000}); err != nil {
		t.Fatal(err)
	}
	top, err = m.CompactionPages(sess.ID)
	if err != nil || len(top) != 5 || top[0].Name != "msg:missing" {
		t.Fatalf("missing-message fallback: %+v %v", top, err)
	}

}
