// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package protocol

import "encoding/json"

// ---------------------------------------------------------------------------
// host.*
// ---------------------------------------------------------------------------

// HostDescription is the host.describe snapshot and readiness probe.
type HostDescription struct {
	Version          string `json:"version"`
	Cwd              string `json:"cwd"`
	Provider         string `json:"provider,omitempty"`
	Model            string `json:"model,omitempty"`
	AttachedSessions int    `json:"attachedSessions"`
	Home             string `json:"home"`
	CanOpenPath      bool   `json:"canOpenPath"`
}

// ---------------------------------------------------------------------------
// sessions
// ---------------------------------------------------------------------------

// SessionSummary is one session.list row.
type SessionSummary struct {
	SessionId       string          `json:"sessionId"`
	UpdatedAt       int64           `json:"updatedAt"`
	Running         bool            `json:"running"`
	Blank           bool            `json:"blank"`
	ParentSessionId string          `json:"parentSessionId,omitempty"`
	Origin          string          `json:"origin,omitempty"`
	Cwd             string          `json:"cwd,omitempty"`
	AgentPreset     string          `json:"agentPreset,omitempty"`
	Projections     json.RawMessage `json:"projections,omitempty"`
}

// SessionListResponse is the session.list value.
type SessionListResponse struct {
	Items []SessionSummary `json:"items"`
}

// SessionCreateRequest is the session.create payload. At most one of
// WorkspaceId/Cwd is set; an omitted project uses the host cwd.
type SessionCreateRequest struct {
	WorkspaceId string `json:"workspaceId,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	SessionId   string `json:"sessionId,omitempty"`
	AgentPreset string `json:"agentPreset,omitempty"`
}

// SessionCreateResponse is the session.create value.
type SessionCreateResponse struct {
	SessionId   string `json:"sessionId"`
	AgentPreset string `json:"agentPreset,omitempty"`
}

// HistoryEntry pairs the raw event with an optional host-computed view.
type HistoryEntry struct {
	Event SessionEvent    `json:"event"`
	View  json.RawMessage `json:"view,omitempty"`
}

// ProjectionsBlock is the tail-page projection baseline.
type ProjectionsBlock struct {
	AsOfSeq int64                      `json:"asOfSeq"`
	Values  map[string]json.RawMessage `json:"values"`
}

// HistoryResponse is the session.history value.
type HistoryResponse struct {
	Events      []HistoryEntry    `json:"events"`
	HasMore     bool              `json:"hasMore"`
	Projections *ProjectionsBlock `json:"projections,omitempty"`
}

// PromptContentPart is one content part of a prompt.
type PromptContentPart struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Data      string `json:"data,omitempty"`
	Name      string `json:"name,omitempty"`
}

// PromptRequest is the session.prompt payload. Mode is "queue"|"steer".
type PromptRequest struct {
	SessionId      string              `json:"sessionId"`
	Mode           string              `json:"mode"`
	Content        []PromptContentPart `json:"content"`
	ClientTimeZone string              `json:"clientTimeZone,omitempty"`
}

// CommandResult is the successful slash-command slot of a prompt value.
type CommandResult struct {
	Kind string `json:"kind"` // "success"
	Text string `json:"text,omitempty"`
}

// PromptResponse is the session.prompt value.
type PromptResponse struct {
	Accepted bool           `json:"accepted"`
	Command  *CommandResult `json:"command,omitempty"`
}

// ModelSelection is a complete model selection.
type ModelSelection struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// ModelReasoningEffort is one selectable reasoning effort.
type ModelReasoningEffort struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ModelReasoning is adapter metadata for one exact model route.
type ModelReasoning struct {
	Efforts       []ModelReasoningEffort `json:"efforts"`
	DefaultEffort string                 `json:"defaultEffort,omitempty"`
}

// ModelCatalogModel is one model inside its provider group.
type ModelCatalogModel struct {
	Id          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Reasoning   *ModelReasoning `json:"reasoning,omitempty"`
}

// ModelProviderGroup is one provider and the models it advertised.
type ModelProviderGroup struct {
	Id     string              `json:"id"`
	Name   string              `json:"name"`
	Models []ModelCatalogModel `json:"models"`
}

// ModelCatalogFailure is a provider whose lookup failed.
type ModelCatalogFailure struct {
	Id      string `json:"id"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// SessionModels is the session.models value.
type SessionModels struct {
	Current  ModelSelection        `json:"current"`
	Routable bool                  `json:"routable"`
	Groups   []ModelProviderGroup  `json:"groups"`
	Failures []ModelCatalogFailure `json:"failures"`
}

// DisplayName resolves one model id to the display name the catalog
// advertises for it — the same face the model picker shows in its roster.
// A group named by provider wins, any other group is the fallback (the
// context's provider may not be among the advertised groups), and the raw
// id stands in when the catalog is nil, unknown, or nameless for the model.
func (m *SessionModels) DisplayName(provider, model string) string {
	if m == nil || model == "" {
		return model
	}
	pick := func(matchProvider bool) string {
		for _, g := range m.Groups {
			if matchProvider && g.Id != provider {
				continue
			}
			for _, mo := range g.Models {
				if mo.Id == model && mo.Name != "" {
					return mo.Name
				}
			}
		}
		return ""
	}
	if n := pick(true); n != "" {
		return n
	}
	if n := pick(false); n != "" {
		return n
	}
	return model
}

// SessionSearchItem is one session.search row.
type SessionSearchItem struct {
	SessionId string `json:"sessionId"`
	Snippet   string `json:"snippet"`
}

// SessionSearchResponse is the session.search value.
type SessionSearchResponse struct {
	Items   []SessionSearchItem `json:"items"`
	HasMore bool                `json:"hasMore"`
}

// QueueAction is a client-requested mutation of one pending queue item.
type QueueAction struct {
	Action  string         `json:"kind"` // "edit" | "remove" | "steer"
	Content []ContentBlock `json:"content,omitempty"`
}

// QueueUpdateRequest is the session.updateQueue payload.
type QueueUpdateRequest struct {
	SessionId string      `json:"sessionId"`
	ItemId    string      `json:"itemId"`
	Action    QueueAction `json:"action"`
}

// SubagentListEntry is one direct child in the subagent.list catalog.
// SubagentListEntry is one row of the subagent.list value: a child
// (one-shot or continuable, with an activity state) or a diagnostic
// placeholder for a row the host could not read.
type SubagentListEntry struct {
	Kind        string `json:"kind"` // "child" | "diagnostic"
	Id          string `json:"id"`
	Mode        string `json:"mode,omitempty"`     // child: "one-shot" | "continuable"
	Activity    string `json:"activity,omitempty"` // child: "running" | "inactive"
	HasChildren bool   `json:"hasChildren,omitempty"`
	Label       string `json:"label,omitempty"`  // child display name
	Reason      string `json:"reason,omitempty"` // diagnostic: "corrupt" | "unsupported" | "unavailable"
}

// SubagentHistoryRequest is the subagent.history payload.
type SubagentHistoryRequest struct {
	ParentSessionId string `json:"parentSessionId"`
	ChildSessionId  string `json:"childSessionId"`
	Mode            string `json:"mode"` // "one-shot" | "continuable"
	BeforeSeq       *int64 `json:"beforeSeq,omitempty"`
	MaxMessages     int    `json:"maxMessages,omitempty"`
}

// SubagentCatalog is the subagent.list value.
type SubagentCatalog struct {
	Entries         []SubagentListEntry `json:"entries"`
	ParentAvailable bool                `json:"parentAvailable"`
}

// SkillEntry is one user-invocable skill (/name in the composer).
type SkillEntry struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	WhenToUse      string `json:"whenToUse,omitempty"`
	ModelInvocable bool   `json:"modelInvocable"`
}

// AgentPresetEntry is one preset the deployment can compose a session from.
type AgentPresetEntry struct {
	Id          string `json:"id"`
	Trust       string `json:"trust"` // system|user
	IsDefault   bool   `json:"isDefault"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Broken      string `json:"broken,omitempty"`
}

// AgentPresetListResponse is the agentPreset.list value.
type AgentPresetListResponse struct {
	Presets    []AgentPresetEntry `json:"presets"`
	Authorable bool               `json:"authorable"`
	HasDoc     bool               `json:"hasDocument"`
}

// ---------------------------------------------------------------------------
// workspaces
// ---------------------------------------------------------------------------

// WorkspaceView is one workspace row: a stable id over a directory path, a
// display title, and the ordered session account (manual ownership order:
// attach prepends, insertSessionBefore reorders; activity never reorders).
type WorkspaceView struct {
	WorkspaceId string   `json:"workspaceId"`
	Path        string   `json:"path"`
	Title       string   `json:"title"`
	SessionIds  []string `json:"sessionIds"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
}

// WorkspaceListResponse is the workspace.list value: the registry in its
// durable display order, plus the registry-global archive set (the
// reconnect baseline of host/archived-sessions-changed).
type WorkspaceListResponse struct {
	Items              []WorkspaceView `json:"items"`
	ArchivedSessionIds []string        `json:"archivedSessionIds"`
}

// WorkspaceCreateRequest registers an EXISTING directory as a workspace
// (no mkdir; a missing or non-directory path fails with
// workspace-invalid-path). A path already owned by a workspace resolves
// idempotently (created=false).
type WorkspaceCreateRequest struct {
	Path string `json:"path"`
}

type WorkspaceCreateResponse struct {
	Workspace *WorkspaceView `json:"workspace"`
	Created   bool           `json:"created"`
}

// WorkspaceRenameRequest renames a workspace; a title clashing with another
// workspace fails with workspace-name-conflict.
type WorkspaceRenameRequest struct {
	WorkspaceId string `json:"workspaceId"`
	Title       string `json:"title"`
}

type WorkspaceRenameResponse struct {
	Workspace *WorkspaceView `json:"workspace"`
}

// WorkspaceDeleteRequest removes one registration; the directory, files and
// session logs remain (their sessions become ungrouped).
type WorkspaceDeleteRequest struct {
	WorkspaceId string `json:"workspaceId"`
}

// WorkspaceMoveRequest moves a workspace within the registry display order
// (DOM-insertBefore-like; an omitted anchor appends to the end).
type WorkspaceMoveRequest struct {
	WorkspaceId       string `json:"workspaceId"`
	BeforeWorkspaceId string `json:"beforeWorkspaceId,omitempty"`
}

// WorkspaceMoveSessionRequest moves one accounted session within its
// workspace's manual order (omitted anchor appends after the tail).
type WorkspaceMoveSessionRequest struct {
	WorkspaceId     string `json:"workspaceId"`
	SessionId       string `json:"sessionId"`
	BeforeSessionId string `json:"beforeSessionId,omitempty"`
}

// WorkspaceArchiveRequest adds one session to the registry-global archive
// set (hidden from grouping surfaces; log and accounting slot remain).
type WorkspaceArchiveRequest struct {
	SessionId string `json:"sessionId"`
}

// ---------------------------------------------------------------------------
// content blocks and messages (model-facing surface)
// ---------------------------------------------------------------------------

// ContentBlock is one model-facing content block.
type ContentBlock struct {
	Type string `json:"type"` // text|reasoning|image|tool-call|tool-result
	Text string `json:"text,omitempty"`
	// tool-call
	Id        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// tool-result
	ToolCallId string         `json:"toolCallId,omitempty"`
	Content    []ContentBlock `json:"content,omitempty"`
	IsError    bool           `json:"isError,omitempty"`
	// image
	Attachment json.RawMessage `json:"attachment,omitempty"`
}

// MessageSource describes where a message came from.
type MessageSource struct {
	Kind     string `json:"kind"` // user|plugin|model|tool|goal|...
	Plugin   string `json:"plugin,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	RpcId    string `json:"rpcId,omitempty"`
	Form     string `json:"form,omitempty"` // context-injection form (snapshot|catalog|notice|...)
}

// Message is one immutable conversation message.
type Message struct {
	Id      string         `json:"id"`
	Role    string         `json:"role"` // user|assistant|system
	Content []ContentBlock `json:"content"`
	Source  MessageSource  `json:"source"`
}

// ---------------------------------------------------------------------------
// session events (the durable log surface)
// ---------------------------------------------------------------------------

// StreamChunk is one raw adapter stream chunk.
type StreamChunk struct {
	Type  string `json:"type"` // block-start|text-delta|reasoning-delta|tool-call-delta|block-end|usage|finish
	Index int    `json:"index,omitempty"`
	Text  string `json:"text,omitempty"`
	// tool-call-delta
	Id             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	ArgumentsDelta string `json:"argumentsDelta,omitempty"`
	// block-end / block-start
	Block *ContentBlock `json:"block,omitempty"`
	// finish
	Reason *FinishReason `json:"reason,omitempty"`
	// usage: the step's token accounting (the adapter emits it before the
	// terminal finish; nothing follows a usage chunk)
	Usage *TokenUsage `json:"usage,omitempty"`
}

// FinishReason is why a model response stopped.
type FinishReason struct {
	Kind    string      `json:"kind"` // stop|tool-calls|max-tokens|aborted|error
	Failure *LlmFailure `json:"failure,omitempty"`
}

// LlmFailure is a structured provider/transport failure.
type LlmFailure struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Status  int    `json:"status,omitempty"`
}

// TokenUsage is per-step token accounting (counts are disjoint).
type TokenUsage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens  int `json:"reasoningTokens,omitempty"`
}

// TokenUsageHost is the host-side cumulative token projection (the
// "tokenUsage" value of session/projection pushes, the tail page's
// projections block, and the roster rows): the session's totals as of
// the frame's seq, input split into uncached and cache-read like the
// host's own accounting (a running step's in-flight sample included).
type TokenUsageHost struct {
	In  int `json:"uncachedInputTokens"`
	Out int `json:"outputTokens"`
	CR  int `json:"cacheReadTokens"`
	CW  int `json:"cacheWriteTokens"`
}

// TurnEndReason is why a turn ended.
type TurnEndReason struct {
	Kind   string      `json:"kind"` // completed|aborted|blocked|error|max-tokens|interrupted
	Reason string      `json:"reason,omitempty"`
	Error  *LlmFailure `json:"error,omitempty"`
}

// TodoItem is one agent todo entry.
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending|in_progress|completed
}

// SessionEvent is one entry of the durable session log. Data is the
// per-type payload, kept raw because the vocabulary is merge-extensible.
type SessionEvent struct {
	Type            string          `json:"type"`
	Seq             int64           `json:"seq"`
	Time            int64           `json:"time"`
	Data            json.RawMessage `json:"data"`
	Ignorable       bool            `json:"ignorable,omitempty"`
	SourceEventSeqs []int           `json:"sourceEventSeqs,omitempty"`
	SurfaceOp       json.RawMessage `json:"surfaceOp,omitempty"`
}

// Event data payloads decoded on demand.

// AssistantMessageEventData is the data of an "assistant/message" event.
type AssistantMessageEventData struct {
	Turn        int         `json:"turn"`
	Step        int         `json:"step"`
	Message     Message     `json:"message"`
	Usage       *TokenUsage `json:"usage,omitempty"`
	Interrupted bool        `json:"interrupted,omitempty"`
}

// ToolCallEventData is the data of a "tool/call" event.
type ToolCallEventData struct {
	Turn      int    `json:"turn"`
	Step      int    `json:"step"`
	CallId    string `json:"callId"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultEventData is the data of a "tool/result" event.
type ToolResultEventData struct {
	Turn    int     `json:"turn"`
	Step    int     `json:"step"`
	Message Message `json:"message"`
	Error   *struct {
		Name string `json:"name"`
		Code string `json:"code"`
	} `json:"error,omitempty"`
	Meta json.RawMessage `json:"meta,omitempty"`
}

// ChunkEventData is the data of an "assistant/chunk" event.
type ChunkEventData struct {
	Turn  int         `json:"turn"`
	Step  int         `json:"step"`
	Chunk StreamChunk `json:"chunk"`
}

// TurnStartEventData is the data of a "turn/start" event.
type TurnStartEventData struct {
	Turn int `json:"turn"`
}

// TurnEndEventData is the data of a "turn/end" event.
type TurnEndEventData struct {
	Turn   int           `json:"turn"`
	Reason TurnEndReason `json:"reason"`
}

// StepEventData is the data of step/start and step/end events.
type StepEventData struct {
	Turn int `json:"turn"`
	Step int `json:"step"`
}

// TodoWriteEventData is the data of a "todo/write" event.
type TodoWriteEventData struct {
	Todos []TodoItem `json:"todos"`
}

// TitleEventData is the data of a "session/title" event.
type TitleEventData struct {
	Title string `json:"title"`
}

// RequestContextData is the data of a "request/context" event.
type RequestContextData struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}

// CommandRunData is the data of a "command/run" event.
type CommandRunData struct {
	CommandId string `json:"commandId"`
	Name      string `json:"name"`
	Args      string `json:"args,omitempty"`
}

// CommandDoneData is the data of a "command/done" event.
type CommandDoneData struct {
	CommandId string `json:"commandId"`
	Kind      string `json:"kind"` // success|error|aborted
	Text      string `json:"text,omitempty"`
}

// GoalSnapshot is the durable goal state inside a goal change.
type GoalSnapshot struct {
	Id            string `json:"id"`
	Revision      int    `json:"revision"`
	Objective     string `json:"objective"`
	Phase         string `json:"phase"` // active|paused|blocked|complete
	BlockedReason *struct {
		Code   string `json:"code"`
		Reason string `json:"reason"`
	} `json:"blockedReason,omitempty"`
	MaxGoalRounds int `json:"maxGoalRounds"`
}

// GoalProjected is the "goal" projection value (durable goal + counters).
type GoalProjected struct {
	Goal          *GoalSnapshot `json:"goal,omitempty"`
	RoundsStarted int           `json:"roundsStarted"`
	CreatedAt     int64         `json:"createdAt,omitempty"`
	UpdatedAt     int64         `json:"updatedAt,omitempty"`
}

// GoalChangeData is the data of a "goal/change" event.
type GoalChangeData struct {
	Kind          string        `json:"kind"` // "goal/change"
	Version       int           `json:"version"`
	Operation     string        `json:"operation"`
	Goal          *GoalSnapshot `json:"goal,omitempty"`
	RoundsStarted int           `json:"roundsStarted,omitempty"`
	Cleared       *struct {
		Id       string `json:"id"`
		Revision int    `json:"revision"`
	} `json:"cleared,omitempty"`
}

// PermissionOption is one selectable preset of the "permissions" session
// projection (dsh-permission-presets): a sandbox-mode + approval-policy
// bundle. "custom" appears in the option list only while it is derived.
type PermissionOption struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PermissionProjection is the "permissions" projection view: the
// deployment's preset table in declaration order plus the currently
// effective preset.
type PermissionProjection struct {
	Options      []PermissionOption `json:"options"`
	CurrentValue string             `json:"currentValue"`
}

// CommandOutcome is the settled result of one host slash-command execution.
type CommandOutcome struct {
	Kind           string `json:"kind"` // "success" | "error"
	Text           string `json:"text,omitempty"`
	SourceEventSeq int64  `json:"sourceEventSeq,omitempty"`
}

// CommandExecResult is the commands/execute value when the slash line
// matched a host command; the value is absent (null) when it matched none.
type CommandExecResult struct {
	CommandId string         `json:"commandId"`
	Result    CommandOutcome `json:"result"`
}

// LlmRetryData is the data of llm/retry and llm/retry-started events.
type LlmRetryData struct {
	Attempt int    `json:"attempt,omitempty"`
	Message string `json:"message,omitempty"`
}

// ---------------------------------------------------------------------------
// jobs
// ---------------------------------------------------------------------------

// JobView is one background job as the client sees it.
type JobView struct {
	Id         string `json:"id"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Status     string `json:"status"` // running|stopping|completed|killed|failed
	Detail     string `json:"detail,omitempty"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
}

// ---------------------------------------------------------------------------
// mux stream frames (payload of /api/events.mux server-requests)
// ---------------------------------------------------------------------------

// MuxSessionEvent is the main live event frame.
type MuxSessionEvent struct {
	Type      string          `json:"type"`
	SessionId string          `json:"sessionId"`
	Event     SessionEvent    `json:"event"`
	View      json.RawMessage `json:"view,omitempty"`
}

// MuxSubscribed is the per-session subscription baseline on stream open.
type MuxSubscribed struct {
	Type      string `json:"type"`
	SessionId string `json:"sessionId"`
	LastSeq   int64  `json:"lastSeq"`
}

// QueuedInboxItem is one pending inbox occurrence.
type QueuedInboxItem struct {
	Id        string  `json:"id"`
	Placement string  `json:"placement"` // queued|steering|context
	Message   Message `json:"message"`
}

// MuxQueue is the authoritative pending-inbox snapshot.
type MuxQueue struct {
	Type      string            `json:"type"`
	SessionId string            `json:"sessionId"`
	Items     []QueuedInboxItem `json:"items"`
}

// MuxJobs is the authoritative job snapshot.
type MuxJobs struct {
	Type      string    `json:"type"`
	SessionId string    `json:"sessionId"`
	Jobs      []JobView `json:"jobs"`
}

// MuxProjection is a live push of one per-session projection value.
type MuxProjection struct {
	Type      string          `json:"type"`
	SessionId string          `json:"sessionId"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Seq       int64           `json:"seq"`
}

// MuxApprovalRequested is an answerable approval frame.
type MuxApprovalRequested struct {
	Type       string `json:"type"`
	SessionId  string `json:"sessionId"`
	ApprovalId string `json:"approvalId"`
	ToolName   string `json:"toolName"`
	CallId     string `json:"callId,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// MuxApprovalResolved is the terminal approval outcome.
type MuxApprovalResolved struct {
	Type       string `json:"type"`
	SessionId  string `json:"sessionId"`
	ApprovalId string `json:"approvalId"`
	Outcome    string `json:"outcome"`
}

// QuestionOption is one selectable answer offered by ask-user.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionIntent is a caller-declared presentation intent.
type QuestionIntent struct {
	Kind    string `json:"kind"` // "plan-review"
	Approve string `json:"approve"`
}

// QuestionItem is one question of an ask-user request.
type QuestionItem struct {
	Id          string           `json:"id"`
	Question    string           `json:"question"`
	Detail      string           `json:"detail,omitempty"`
	Header      string           `json:"header,omitempty"`
	Options     []QuestionOption `json:"options,omitempty"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
	Intent      *QuestionIntent  `json:"intent,omitempty"`
}

// MuxQuestionRequested is an answerable question frame.
type MuxQuestionRequested struct {
	Type      string         `json:"type"`
	SessionId string         `json:"sessionId"`
	Questions []QuestionItem `json:"questions"`
}

// MuxQuestionResolved is the terminal question outcome.
type MuxQuestionResolved struct {
	Type          string `json:"type"`
	SessionId     string `json:"sessionId"`
	QuestionRpcId string `json:"questionRpcId"`
	Outcome       string `json:"outcome"` // answered|cancelled
}

// StreamError is a stream-level failure frame.
type StreamError struct {
	Type  string   `json:"type"`
	Error RpcError `json:"error"`
}

// ---------------------------------------------------------------------------
// host stream frames (payload of /api/events.host server-requests)
// ---------------------------------------------------------------------------

// HostFrame is the union of host-stream payloads, discriminated by Type.
type HostFrame struct {
	Type string `json:"type"`
	// host/session-added, host/session-status, host/agent-error
	SessionId       string `json:"sessionId,omitempty"`
	Blank           bool   `json:"blank,omitempty"`
	Running         bool   `json:"running,omitempty"`
	Message         string `json:"message,omitempty"`
	ParentSessionId string `json:"parentSessionId,omitempty"`
	Origin          string `json:"origin,omitempty"`
	Cwd             string `json:"cwd,omitempty"`
	AgentPreset     string `json:"agentPreset,omitempty"`
	// host/workspace-changed
	Workspace json.RawMessage `json:"workspace,omitempty"`
	// host/workspace-removed
	WorkspaceId string `json:"workspaceId,omitempty"`
	// host/workspace-order-changed: the complete durable display order.
	WorkspaceIds []string `json:"workspaceIds,omitempty"`
	// host/archived-sessions-changed: the complete registry-global archive set.
	ArchivedSessionIds []string `json:"archivedSessionIds,omitempty"`
}

// ---------------------------------------------------------------------------
// respond payloads (client-response values)
// ---------------------------------------------------------------------------

// ApprovalResponse answers an approval/requested frame.
type ApprovalResponse struct {
	SessionId  string `json:"sessionId"`
	ApprovalId string `json:"approvalId"`
	Outcome    string `json:"outcome"` // "allowed-once" | "rejected"
}

// QuestionAnswerItem answers one question.
type QuestionAnswerItem struct {
	Id       string   `json:"id"`
	Selected []string `json:"selected"`
	Custom   string   `json:"custom,omitempty"`
}

// QuestionAnswer answers a question/requested frame as one batch.
type QuestionAnswer struct {
	SessionId string `json:"sessionId"`
	Answer    struct {
		Answers []QuestionAnswerItem `json:"answers"`
	} `json:"answer"`
}
