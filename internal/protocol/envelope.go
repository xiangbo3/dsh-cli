// Package protocol carries the wire vocabulary of the DeepSeek Harness web
// server (dsh-host-apiproxy contract): the four-quadrant RPC envelope, the
// unary method registry, and the downlink frame decoders.
package protocol

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Quadrants: who initiates × request/response.
const (
	TypeClientRequest  = "client-request"
	TypeServerResponse = "server-response"
	TypeServerRequest  = "server-request"
	TypeClientResponse = "client-response"
)

// Envelope is one wire message in any quadrant.
type Envelope struct {
	Type    string          `json:"type"`
	RpcId   string          `json:"rpcId"`
	Method  string          `json:"method,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Result  *Result         `json:"result,omitempty"`
}

// Result is the business success/failure slot of a response.
type Result struct {
	Ok    bool            `json:"ok"`
	Value json.RawMessage `json:"value,omitempty"`
	Error *RpcError       `json:"error,omitempty"`
}

// RpcError is a business error; Code is a closed vocabulary.
type RpcError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (e *RpcError) Error() string { return "rpc " + e.Code + ": " + e.Message }

// NewRPCId mints a UUID v4 correlation id.
func NewRPCId() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("rpc-%d", nowMillis())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	const hexd = "0123456789abcdef"
	var sb strings.Builder
	sb.Grow(36)
	for i, c := range b {
		switch i {
		case 4, 6, 8, 10:
			sb.WriteByte('-')
		}
		sb.WriteByte(hexd[c>>4])
		sb.WriteByte(hexd[c&0x0f])
	}
	return sb.String()
}

// Error codes (closed set, see RpcErrorDetailsMap in the contract).
const (
	ErrBadRequest      = "bad-request"
	ErrCancelled       = "cancelled"
	ErrSessionNotFound = "session-not-found"
	ErrModelUnavail    = "model-unavailable"
	ErrSessionConflict = "session-conflict"
	ErrAgentBusy       = "agent-busy"
	ErrSteerUnavail    = "steer-unavailable"
	ErrQueueItemFound  = "queue-item-not-found"
	ErrCommandError    = "command-error"
	ErrUnknownCommand  = "unknown-command"
	ErrSettingsReject  = "settings-rejected"
	ErrSettingsConflic = "settings-conflict"
)

// Methods — the wire path segments of RpcMethodMap.
const (
	MHostDescribe = "host.describe"
	MHostListDir  = "host.listDirectory"
	MHostMakeDir  = "host.createDirectory"
	MHostOpenPath = "host.openPath"
	MHostPickDir  = "host.pickDirectory"

	MSessionList        = "session.list"
	MSessionSearch      = "session.search"
	MSessionCreate      = "session.create"
	MSessionHistory     = "session.history"
	MSessionModels      = "session.models"
	MSessionSelectModel = "session.selectModel"
	MSessionRename      = "session.rename"
	MSessionFork        = "session.fork"
	MSessionPrompt      = "session.prompt"
	MSessionAttachment  = "session.attachment"
	MSessionUpdateQueue = "session.updateQueue"
	MSessionCancel      = "session.cancel"

	MSubagentList      = "subagent.list"
	MSubagentHistory   = "subagent.history"
	MSubagentPrompt    = "subagent.prompt"
	MSubagentInterrupt = "subagent.interrupt"

	MWorkspaceList   = "workspace.list"
	MWorkspaceCreate = "workspace.create"
	MWorkspaceRename = "workspace.rename"
	MWorkspaceDelete = "workspace.delete"
	MWorkspaceMove   = "workspace.insertBefore"
	MWorkspaceMoveSe = "workspace.insertSessionBefore"
	MWorkspaceArch   = "workspace.archiveSession"

	MAgentPresetList   = "agentPreset.list"
	MAgentPresetSelect = "agentPreset.select"

	MSkillList = "skill.list"

	MGoalCreate   = "goal.create"
	MGoalEdit     = "goal.edit"
	MGoalPause    = "goal.pause"
	MGoalResume   = "goal.resume"
	MGoalComplete = "goal.complete"
	MGoalClear    = "goal.clear"

	MSettingsDescribe    = "settings.describe"
	MSettingsOpenDoc     = "settings.openDocument"
	MSettingsUpdate      = "settings.update"
	MSettingsReplace     = "settings.replace"
	MSettingsMutate      = "settings.mutate"
	MCredentialsDescribe = "credentials.describe"
	MCredentialsSet      = "credentials.set"
	MCredentialsUnset    = "credentials.unset"
	MLlmProviders        = "llm.providers"
	MLlmModels           = "llm.models"
)

// Downlink stream paths and frame types.
const (
	StreamMux   = "/api/events.mux"
	StreamHost  = "/api/events.host"
	PathRespond = "/api/respond"
)

// Mux stream frame types.
const (
	FMuxEvent       = "session/event"
	FMuxSubscribed  = "session/subscribed"
	FMuxApprovalReq = "approval/requested"
	FMuxApprovalRes = "approval/resolved"
	FMuxQuestionReq = "question/requested"
	FMuxQuestionRes = "question/resolved"
	FMuxQueue       = "session/queue"
	FMuxJobs        = "session/jobs"
	FMuxProjection  = "session/projection"
	FStreamError    = "stream/error"
)

// Host stream frame types.
const (
	FHostSessionAdded     = "host/session-added"
	FHostSessionRemoved   = "host/session-removed"
	FHostSessionStatus    = "host/session-status"
	FHostAgentError       = "host/agent-error"
	FHostWorkspaceChanged = "host/workspace-changed"
	FHostWorkspaceRemoved = "host/workspace-removed"
	FHostOrderChanged     = "host/workspace-order-changed"
	FHostArchived         = "host/archived-sessions-changed"
	FHostRemoteEvent      = "host/remote-event"
)

// nowMillis is var-seam for tests.
var nowMillis = func() int64 { return time.Now().UnixMilli() }
