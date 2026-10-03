package permission

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/google/uuid"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/pubsub"
)

var ErrorPermissionDenied = errors.New("permission denied")

type CreatePermissionRequest struct {
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
	// Fingerprint identifies the specific action being approved, so a
	// session-wide grant covers only that action rather than everything the
	// tool could do in the same directory. Tools whose Path is effectively
	// constant for a session MUST set it — Bash (Path is always the working
	// directory) sets the command, Fetch sets the URL — otherwise approving
	// one command would silently authorize every later command in the
	// session. File-editing tools leave it empty on purpose: their Path is
	// the target file's directory, which is already a meaningful scope.
	Fingerprint string `json:"fingerprint,omitempty"`
}

type PermissionRequest struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type Service interface {
	pubsub.Suscriber[PermissionRequest]
	GrantPersistant(permission PermissionRequest)
	Grant(permission PermissionRequest)
	Deny(permission PermissionRequest)
	Request(opts CreatePermissionRequest) bool
	AutoApproveSession(sessionID string)
	// DenyAllSession makes every request in the session return false without
	// prompting, and records it for Denied. For non-interactive runs, where
	// nobody is there to answer a prompt.
	DenyAllSession(sessionID string)
	// LinkSession makes a subagent's session follow its parent's
	// AutoApproveSession or DenyAllSession mode.
	LinkSession(childID, parentID string)
	// Denied returns, in order, the requests DenyAllSession refused in the
	// session and every session linked beneath it.
	Denied(sessionID string) []PermissionRequest
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	// mu guards sessionPermissions, the session modes, parents, and denied:
	// grants are appended from the TUI goroutine while Request reads them from
	// the agent goroutine.
	mu                  sync.RWMutex
	sessionPermissions  []PermissionRequest
	pendingRequests     sync.Map
	autoApproveSessions []string
	denyAllSessions     []string
	parents             map[string]string
	denied              map[string][]PermissionRequest

	// promptMu serializes the interactive part of Request: the grant check, the
	// publish, and the wait for an answer. The UI can only present one approval
	// dialog at a time, so concurrent callers (tool calls now run in parallel)
	// must queue rather than race two dialogs into the same surface.
	//
	// Holding it across the grant check also deduplicates: if two concurrent
	// calls need the same approval and the user picks "allow for session", the
	// second one finds the fresh grant instead of asking again.
	promptMu sync.Mutex
}

// safeWorkingDir returns the configured working directory, falling back to the
// process working directory when config has not been loaded. config.WorkingDirectory
// panics in that case, and a permission check is the last place that should
// bring down the process.
func safeWorkingDir() (dir string) {
	defer func() {
		if recover() != nil {
			if cwd, err := os.Getwd(); err == nil {
				dir = cwd
			}
		}
	}()
	return config.WorkingDirectory()
}

// GrantPersistant approves this request and records the grant for the rest of
// the session.
//
// The grant is recorded before the waiting caller is woken, and the order
// matters: Request holds promptMu across its grant check and its wait, so the
// moment the waiter is signalled it can return and release promptMu. Recording
// afterwards left a window where a queued caller needing the same approval ran
// its grant check against a list the grant had not reached yet, and prompted a
// second time. See promptMu.
func (s *permissionService) GrantPersistant(permission PermissionRequest) {
	s.mu.Lock()
	s.sessionPermissions = append(s.sessionPermissions, permission)
	s.mu.Unlock()

	if respCh, ok := s.pendingRequests.Load(permission.ID); ok {
		respCh.(chan bool) <- true
	}
}

func (s *permissionService) Grant(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- true
	}
}

func (s *permissionService) Deny(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- false
	}
}

func (s *permissionService) Request(opts CreatePermissionRequest) bool {
	s.mu.RLock()
	autoApproved, deniedRoot := s.sessionModeLocked(opts.SessionID)
	s.mu.RUnlock()
	if autoApproved {
		return true
	}
	dir := filepath.Dir(opts.Path)
	if dir == "." {
		dir = safeWorkingDir()
	}
	permission := PermissionRequest{
		ID:          uuid.New().String(),
		Path:        dir,
		SessionID:   opts.SessionID,
		ToolName:    opts.ToolName,
		Description: opts.Description,
		Action:      opts.Action,
		Params:      opts.Params,
		Fingerprint: opts.Fingerprint,
	}
	if deniedRoot != "" {
		s.mu.Lock()
		s.denied[deniedRoot] = append(s.denied[deniedRoot], permission)
		s.mu.Unlock()
		return false
	}

	// One dialog at a time. See promptMu.
	s.promptMu.Lock()
	defer s.promptMu.Unlock()

	// Re-checked under promptMu rather than before it, so a grant issued while
	// this caller was queued is honoured instead of prompting twice.
	if s.hasSessionGrant(permission) {
		return true
	}

	respCh := make(chan bool, 1)

	s.pendingRequests.Store(permission.ID, respCh)
	defer s.pendingRequests.Delete(permission.ID)

	s.Publish(pubsub.CreatedEvent, permission)

	// Wait for the response with a timeout
	resp := <-respCh
	return resp
}

// hasSessionGrant reports whether an earlier "allow for session" already covers
// this exact request. Fingerprint participates in the match, so a grant for one
// command or URL never authorizes a different one.
func (s *permissionService) hasSessionGrant(req PermissionRequest) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.sessionPermissions {
		if p.ToolName == req.ToolName &&
			p.Action == req.Action &&
			p.SessionID == req.SessionID &&
			p.Path == req.Path &&
			p.Fingerprint == req.Fingerprint {
			return true
		}
	}
	return false
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.mu.Lock()
	s.autoApproveSessions = append(s.autoApproveSessions, sessionID)
	s.mu.Unlock()
}

func (s *permissionService) DenyAllSession(sessionID string) {
	s.mu.Lock()
	s.denyAllSessions = append(s.denyAllSessions, sessionID)
	s.mu.Unlock()
}

func (s *permissionService) LinkSession(childID, parentID string) {
	if childID == "" || parentID == "" || childID == parentID {
		return
	}
	s.mu.Lock()
	s.parents[childID] = parentID
	s.mu.Unlock()
}

func (s *permissionService) Denied(sessionID string) []PermissionRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.denied[sessionID])
}

// sessionModeLocked walks from sessionID up through linked parents. It
// reports whether the nearest session with a mode auto-approves, or else the
// ID of the deny-all session that denials are recorded under. Caller holds mu.
func (s *permissionService) sessionModeLocked(sessionID string) (autoApproved bool, deniedRoot string) {
	seen := map[string]bool{}
	for id := sessionID; id != "" && !seen[id]; id = s.parents[id] {
		seen[id] = true
		if slices.Contains(s.autoApproveSessions, id) {
			return true, ""
		}
		if slices.Contains(s.denyAllSessions, id) {
			return false, id
		}
	}
	return false, ""
}

func NewPermissionService() Service {
	return &permissionService{
		Broker:             pubsub.NewBroker[PermissionRequest](),
		sessionPermissions: make([]PermissionRequest, 0),
		parents:            map[string]string{},
		denied:             map[string][]PermissionRequest{},
	}
}
