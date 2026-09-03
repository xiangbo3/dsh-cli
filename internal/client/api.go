package client

import (
	"context"

	"dsh-cli/internal/protocol"
)

// Typed session/host facade over Client.call.

// Describe is the readiness probe.
func (c *Client) Describe(ctx context.Context) (*protocol.HostDescription, error) {
	var v protocol.HostDescription
	if err := c.call(ctx, protocol.MHostDescribe, struct{}{}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// ListSessions returns all persisted sessions, updatedAt descending.
func (c *Client) ListSessions(ctx context.Context) (*protocol.SessionListResponse, error) {
	var v protocol.SessionListResponse
	if err := c.call(ctx, protocol.MSessionList, struct {
		Cursor string `json:"cursor,omitempty"`
	}{}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// CreateSession creates a session; an omitted cwd uses the host cwd.
func (c *Client) CreateSession(ctx context.Context, req protocol.SessionCreateRequest) (*protocol.SessionCreateResponse, error) {
	var v protocol.SessionCreateResponse
	if err := c.call(ctx, protocol.MSessionCreate, req, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// History reads a window of history events. beforeSeq==0 requests the tail
// page (which also carries the projection baseline).
func (c *Client) History(ctx context.Context, sessionId string, beforeSeq int64, maxMessages int) (*protocol.HistoryResponse, error) {
	payload := struct {
		SessionId   string `json:"sessionId"`
		BeforeSeq   int64  `json:"beforeSeq,omitempty"`
		MaxMessages int    `json:"maxMessages,omitempty"`
	}{SessionId: sessionId, BeforeSeq: beforeSeq, MaxMessages: maxMessages}
	var v protocol.HistoryResponse
	if err := c.call(ctx, protocol.MSessionHistory, payload, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// CommandExecute runs one slash-command line against a session's agent
// without a model turn (the host command registry). matched is false when
// the line resolved to no command (the deployment has no such command).
func (c *Client) CommandExecute(ctx context.Context, sessionId, line string) (res *protocol.CommandExecResult, matched bool, err error) {
	payload := struct {
		Args struct {
			AgentId string `json:"agentId"`
			Line    string `json:"line"`
			Images  []any  `json:"images"`
		} `json:"args"`
	}{}
	payload.Args.AgentId = sessionId
	payload.Args.Line = line
	payload.Args.Images = []any{}
	var v *protocol.CommandExecResult
	if err := c.call(ctx, "commands/execute", payload, &v); err != nil {
		return nil, false, err
	}
	if v == nil {
		return nil, false, nil
	}
	return v, true, nil
}

// Prompt sends a message to a session (mode "queue" or "steer").
func (c *Client) Prompt(ctx context.Context, sessionId, text, mode string) (*protocol.PromptResponse, error) {
	req := protocol.PromptRequest{
		SessionId: sessionId,
		Mode:      mode,
		Content:   []protocol.PromptContentPart{{Type: "text", Text: text}},
	}
	var v protocol.PromptResponse
	if err := c.call(ctx, protocol.MSessionPrompt, req, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Cancel stops the active turn, preserving pending inbox work.
func (c *Client) Cancel(ctx context.Context, sessionId string) error {
	return c.call(ctx, protocol.MSessionCancel, struct {
		SessionId string `json:"sessionId"`
	}{sessionId}, nil)
}

// Models reads a fresh advisory model directory for the session.
func (c *Client) Models(ctx context.Context, sessionId string) (*protocol.SessionModels, error) {
	var v protocol.SessionModels
	if err := c.call(ctx, protocol.MSessionModels, struct {
		SessionId string `json:"sessionId"`
	}{sessionId}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// SelectModel sets the session's model selection.
func (c *Client) SelectModel(ctx context.Context, sessionId, provider, model, reasoningEffort string) (*protocol.ModelSelection, error) {
	payload := struct {
		SessionId       string `json:"sessionId"`
		Provider        string `json:"provider"`
		Model           string `json:"model"`
		ReasoningEffort string `json:"reasoningEffort,omitempty"`
	}{sessionId, provider, model, reasoningEffort}
	var v struct {
		Selected protocol.ModelSelection `json:"selected"`
	}
	if err := c.call(ctx, protocol.MSessionSelectModel, payload, &v); err != nil {
		return nil, err
	}
	return &v.Selected, nil
}

// Rename pins an explicit session title.
func (c *Client) Rename(ctx context.Context, sessionId, title string) (string, error) {
	var v struct {
		Title string `json:"title"`
		Seq   int64  `json:"seq"`
	}
	if err := c.call(ctx, protocol.MSessionRename, struct {
		SessionId string `json:"sessionId"`
		Title     string `json:"title"`
	}{sessionId, title}, &v); err != nil {
		return "", err
	}
	return v.Title, nil
}

// Fork starts a new session from a completed-turn prefix of the source.
// atSeq<=0 falls back to the source's last completed turn.
func (c *Client) Fork(ctx context.Context, sessionId string, atSeq int64) (string, error) {
	payload := struct {
		SessionId string `json:"sessionId"`
		AtSeq     int64  `json:"atSeq,omitempty"`
	}{sessionId, atSeq}
	var v struct {
		SessionId string `json:"sessionId"`
	}
	if err := c.call(ctx, protocol.MSessionFork, payload, &v); err != nil {
		return "", err
	}
	return v.SessionId, nil
}

// Search runs a bounded content search across visible sessions.
func (c *Client) Search(ctx context.Context, query string) (*protocol.SessionSearchResponse, error) {
	var v protocol.SessionSearchResponse
	if err := c.call(ctx, protocol.MSessionSearch, struct {
		Query string `json:"query"`
	}{query}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// UpdateQueue edits, removes, or strictly steers one pending queue item.
func (c *Client) UpdateQueue(ctx context.Context, req protocol.QueueUpdateRequest) error {
	return c.call(ctx, protocol.MSessionUpdateQueue, req, nil)
}

// Subagents lists direct session-backed children of a parent.
func (c *Client) Subagents(ctx context.Context, parentSessionId string) (*protocol.SubagentCatalog, error) {
	var v protocol.SubagentCatalog
	if err := c.call(ctx, protocol.MSubagentList, struct {
		ParentSessionId string `json:"parentSessionId"`
	}{parentSessionId}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// ---- workspaces ---------------------------------------------------------

// Workspaces reads the registry baseline: workspaces in durable display
// order, plus the registry-global archive set.
func (c *Client) Workspaces(ctx context.Context) (*protocol.WorkspaceListResponse, error) {
	var v protocol.WorkspaceListResponse
	if err := c.call(ctx, protocol.MWorkspaceList, struct{}{}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// WorkspaceCreate registers an existing directory as a workspace
// (idempotent: an already-owned path resolves with Created=false).
func (c *Client) WorkspaceCreate(ctx context.Context, path string) (*protocol.WorkspaceCreateResponse, error) {
	var v protocol.WorkspaceCreateResponse
	if err := c.call(ctx, protocol.MWorkspaceCreate, protocol.WorkspaceCreateRequest{Path: path}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// WorkspaceRename sets a workspace's display title.
func (c *Client) WorkspaceRename(ctx context.Context, workspaceID, title string) (*protocol.WorkspaceView, error) {
	var v protocol.WorkspaceRenameResponse
	if err := c.call(ctx, protocol.MWorkspaceRename, protocol.WorkspaceRenameRequest{WorkspaceId: workspaceID, Title: title}, &v); err != nil {
		return nil, err
	}
	return v.Workspace, nil
}

// WorkspaceDelete removes one registration (sessions fall ungrouped).
func (c *Client) WorkspaceDelete(ctx context.Context, workspaceID string) error {
	return c.call(ctx, protocol.MWorkspaceDelete, protocol.WorkspaceDeleteRequest{WorkspaceId: workspaceID}, nil)
}

// WorkspaceMove reorders the registry display order (omitted anchor appends).
func (c *Client) WorkspaceMove(ctx context.Context, req protocol.WorkspaceMoveRequest) ([]string, error) {
	var v struct {
		WorkspaceIds []string `json:"workspaceIds"`
	}
	if err := c.call(ctx, protocol.MWorkspaceMove, req, &v); err != nil {
		return nil, err
	}
	return v.WorkspaceIds, nil
}

// WorkspaceMoveSession reorders one session within its workspace account.
func (c *Client) WorkspaceMoveSession(ctx context.Context, req protocol.WorkspaceMoveSessionRequest) (*protocol.WorkspaceView, error) {
	var v protocol.WorkspaceRenameResponse
	if err := c.call(ctx, protocol.MWorkspaceMoveSe, req, &v); err != nil {
		return nil, err
	}
	return v.Workspace, nil
}

// WorkspaceArchiveSession adds one session to the registry-global archive set.
func (c *Client) WorkspaceArchiveSession(ctx context.Context, sessionID string) ([]string, error) {
	var v struct {
		ArchivedSessionIds []string `json:"archivedSessionIds"`
	}
	if err := c.call(ctx, protocol.MWorkspaceArch, protocol.WorkspaceArchiveRequest{SessionId: sessionID}, &v); err != nil {
		return nil, err
	}
	return v.ArchivedSessionIds, nil
}

// Skills lists the user-invocable skill catalog for the session's project.
func (c *Client) Skills(ctx context.Context, sessionId string) ([]protocol.SkillEntry, error) {
	var v struct {
		Skills []protocol.SkillEntry `json:"skills"`
	}
	if err := c.call(ctx, protocol.MSkillList, struct {
		SessionId string `json:"sessionId"`
	}{sessionId}, &v); err != nil {
		return nil, err
	}
	return v.Skills, nil
}

// AgentPresets lists the composition presets for new sessions.
func (c *Client) AgentPresets(ctx context.Context) (*protocol.AgentPresetListResponse, error) {
	var v protocol.AgentPresetListResponse
	if err := c.call(ctx, protocol.MAgentPresetList, struct{}{}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// SelectAgentPreset recomposes one session's agent from another preset.
// Allowed only while the session is blank (no turn has run; the host
// answers agent-preset-locked otherwise). Returns the preset id now in
// force for the session.
func (c *Client) SelectAgentPreset(ctx context.Context, sessionId, agentPreset string) (string, error) {
	var v struct {
		AgentPreset string `json:"agentPreset"`
	}
	if err := c.call(ctx, protocol.MAgentPresetSelect, struct {
		SessionId   string `json:"sessionId"`
		AgentPreset string `json:"agentPreset"`
	}{sessionId, agentPreset}, &v); err != nil {
		return "", err
	}
	return v.AgentPreset, nil
}
