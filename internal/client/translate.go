// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Wire translation for the cookie-gated (new) host build. The new build
// renames the method paths (ns.method → ns/method), wraps every payload
// in an args object keyed by the parameter name, and reshapes a few
// values. The translation keeps the facade signatures stable: the old
// payload in, the old value shape out.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"dsh-cli/internal/protocol"
)

// wrapNone sends an empty args object (the endpoint takes no parameters).
const (
	wrapNone     = ""
	wrapRequest  = "request"
	wrapReserved = "_request" // the reserved empty-list request slot
)

// newRoute is one old method's new-dialect address.
type newRoute struct {
	path string
	wrap string // args key for the old payload ("" = no args, special = custom)
}

// newRoutes covers every method the facades speak. Methods absent here
// fall to their no-args or passthrough default (see newArgs).
var newRoutes = map[string]newRoute{
	"session.list":                  {"session/list", wrapReserved},
	"session.history":               {"session/page", "page"},
	"subagent.history":              {"session/page", "subpage"},
	"session.prompt":                {"session/prompt", "prompt"},
	"session.models":                {"session/modelCatalog", "models"},
	"session.search":                {"session/search", wrapRequest},
	"session.create":                {"session/create", wrapRequest},
	"session.cancel":                {"session/cancel", wrapRequest},
	"session.selectModel":           {"session/selectModel", wrapRequest},
	"session.rename":                {"session/rename", wrapRequest},
	"session.fork":                  {"session/fork", wrapRequest},
	"session.updateQueue":           {"session/updateQueue", wrapRequest},
	"session.attachment":            {"session/attachment", wrapRequest},
	"subagent.list":                 {"subagents/list", "top"},
	"subagent.prompt":               {"subagents/prompt", wrapRequest},
	"workspace.create":              {"workspace/create", wrapRequest},
	"workspace.rename":              {"workspace/rename", wrapRequest},
	"workspace.delete":              {"workspace/delete", wrapRequest},
	"workspace.insertBefore":        {"workspace/insertBefore", wrapRequest},
	"workspace.insertSessionBefore": {"workspace/insertSessionBefore", wrapRequest},
	"workspace.archiveSession":      {"workspace/archiveSession", wrapRequest},
	"skill.list":                    {"skills/list", wrapRequest},
	"agentPreset.list":              {"agentPresets/list", wrapNone},
	"host.listDirectory":            {"directoryPicker/list", "path"},
	"host.createDirectory":          {"directoryPicker/createDirectory", "path"},
	"host.openPath":                 {"session/openWorkspacePath", wrapRequest},
	"host.pickDirectory":            {"directoryPicker/pick", wrapNone},
	"settings.describe":             {"settings/describe", "top"},
	"settings.openDocument":         {"settings/openSettingsDocument", wrapNone},
	"settings.update":               {"settings/update", "top"},
	"settings.replace":              {"settings/replace", "top"},
	"settings.mutate":               {"settings/mutate", "top"},
	"credentials.describe":          {"credentials/describe", "top"},
	"credentials.set":               {"credentials/set", "top"},
	"credentials.unset":             {"credentials/unset", "top"},
	"llm.providers":                 {"llm/listProviders", wrapNone},
	"llm.models":                    {"llm/listModels", "provider"},
	"goal.create":                   {"goals/create", "goal"},
	"goal.edit":                     {"goals/edit", "goalRef"},
	"goal.pause":                    {"goals/pause", "goalRef"},
	"goal.resume":                   {"goals/resume", "goalRef"},
	"goal.complete":                 {"goals/complete", "goalRef"},
	"goal.clear":                    {"goals/clear", "goalRef"},
}

// newArgs builds the new payload ({args: …}) for one old payload.
// rpcId is the envelope id of this call (the prompt requestId reuses it:
// the host echoes it into the user message source, and the text-based
// echo reconcile covers the id mismatch).
func newArgs(method string, rawPayload []byte, rpcId string) ([]byte, error) {
	route, known := newRoutes[method]
	switch method {
	case "session.models":
		// The new catalog takes no session parameter: it is the
		// deployment-wide model directory (the old per-session call
		// maps onto it; the facade ignores the session).
		return []byte(`{"args":{}}`), nil
	case "session.history":
		var p struct {
			SessionId   string `json:"sessionId"`
			BeforeSeq   int64  `json:"beforeSeq"`
			MaxMessages int    `json:"maxMessages"`
		}
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		req := map[string]any{
			"address": map[string]any{"kind": "session", "sessionId": p.SessionId},
			// throughSeq starts as the placeholder the cursor patcher
			// replaces (the page endpoint requires the field to ride).
			"throughSeq": 0,
		}
		// beforeSeq 0 = the tail page; the paginate endpoint reads a
		// zero edge as "nothing before it" (an empty page), so it rides
		// only when the caller actually pages backwards.
		if p.BeforeSeq > 0 {
			req["beforeSeq"] = p.BeforeSeq
		}
		if p.MaxMessages > 0 {
			req["maxMessages"] = p.MaxMessages
		}
		return json.Marshal(map[string]any{"args": map[string]any{"request": req}})
	case "subagent.history":
		var p protocol.SubagentHistoryRequest
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		req := map[string]any{
			"address": map[string]any{
				"kind":            "subagent",
				"parentSessionId": p.ParentSessionId,
				"childSessionId":  p.ChildSessionId,
				"mode":            p.Mode,
			},
			"throughSeq": 0,
		}
		if p.BeforeSeq != nil {
			req["beforeSeq"] = *p.BeforeSeq
		}
		if p.MaxMessages > 0 {
			req["maxMessages"] = p.MaxMessages
		}
		return json.Marshal(map[string]any{"args": map[string]any{"request": req}})
	case "session.prompt":
		var p protocol.PromptRequest
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		req, _ := json.Marshal(p)
		var reqObj map[string]any
		if err := json.Unmarshal(req, &reqObj); err != nil {
			return nil, err
		}
		reqObj["requestId"] = rpcId
		return json.Marshal(map[string]any{"args": map[string]any{"request": reqObj}})
	case "subagent.interrupt":
		var p struct {
			ParentSessionId string `json:"parentSessionId"`
			ChildSessionId  string `json:"childSessionId"`
		}
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"args": map[string]any{
			"childSessionId":  p.ChildSessionId,
			"parentSessionId": p.ParentSessionId,
			"mode":            "continuable",
		}})
	case "agentPreset.select":
		var p struct {
			SessionId   string `json:"sessionId"`
			AgentPreset string `json:"agentPreset"`
		}
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"args": map[string]any{
			"agentId":     p.SessionId,
			"agentPreset": p.AgentPreset,
		}})
	case "commands/execute":
		// The old payload is already the new args shape; pin the shape
		// so a regression fails here, not at the host.
		var obj map[string]any
		if err := json.Unmarshal(rawPayload, &obj); err != nil {
			return nil, err
		}
		if _, ok := obj["args"]; !ok {
			return nil, fmt.Errorf("commands/execute: payload lacks the args slot")
		}
		return rawPayload, nil
	}
	if !known {
		return nil, fmt.Errorf("new-dialect: no route for %q", method)
	}
	switch route.wrap {
	case wrapNone:
		return []byte(`{"args":{}}`), nil
	case "top":
		// The parameter names match the old fields: send the payload as
		// the args object directly.
		return json.Marshal(map[string]any{"args": json.RawMessage(rawPayload)})
	case "path":
		var p struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		args := map[string]any{"path": p.Path}
		if p.Name != "" {
			args["name"] = p.Name
		}
		return json.Marshal(map[string]any{"args": args})
	case "provider":
		var p struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"args": map[string]any{"provider": p.Provider}})
	case "goal":
		var p map[string]any
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		args := map[string]any{"request": p}
		if sid, ok := p["sessionId"]; ok {
			args["agentId"] = sid
		}
		return json.Marshal(map[string]any{"args": args})
	case "goalRef":
		var p map[string]any
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return nil, err
		}
		ref := p
		args := map[string]any{"ref": ref}
		if sid, ok := p["sessionId"]; ok {
			args["agentId"] = sid
			delete(ref, "sessionId")
		}
		return json.Marshal(map[string]any{"args": args})
	default: // wrapRequest and kin: {<wrap>: payload}
		return json.Marshal(map[string]any{"args": map[string]any{route.wrap: json.RawMessage(rawPayload)}})
	}
}

// newValue remaps a new-dialect result value to the old facade shape.
// A nil remap is the identity (most values share their shape across
// builds).
func newValue(method string, value []byte) ([]byte, error) {
	switch method {
	case "session.history", "subagent.history":
		var p struct {
			Records []struct {
				Type  string                `json:"type"`
				Event protocol.SessionEvent `json:"event"`
			} `json:"records"`
			HasMore bool `json:"hasMore"`
		}
		if err := json.Unmarshal(value, &p); err != nil {
			return nil, err
		}
		events := make([]protocol.HistoryEntry, 0, len(p.Records))
		for _, r := range p.Records {
			if r.Type != "event" {
				continue // chunk runs fold identically by their events
			}
			events = append(events, protocol.HistoryEntry{Event: r.Event})
		}
		return json.Marshal(protocol.HistoryResponse{Events: events, HasMore: p.HasMore})
	case "session.models":
		var p struct {
			Default           protocol.ModelSelection        `json:"default"`
			RoutableProviders []string                       `json:"routableProviders"`
			Groups            []protocol.ModelProviderGroup  `json:"groups"`
			Failures          []protocol.ModelCatalogFailure `json:"failures"`
		}
		if err := json.Unmarshal(value, &p); err != nil {
			return nil, err
		}
		return json.Marshal(protocol.SessionModels{
			Current:  p.Default,
			Routable: len(p.Groups) > 0,
			Groups:   p.Groups,
			Failures: p.Failures,
		})
	case "session.selectModel":
		var p struct {
			Selected protocol.ModelSelection `json:"selected"`
		}
		if err := json.Unmarshal(value, &p); err != nil {
			return nil, err
		}
		return json.Marshal(p.Selected)
	case "agentPreset.select":
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"agentPreset": s})
	default:
		return value, nil
	}
}

// describeNew composes the host snapshot the old host answered as one
// RPC: the new build publishes the facts across three endpoints and the
// stream's ready frame (home).
func (c *Client) describeNew(ctx context.Context) (*protocol.HostDescription, error) {
	var cat struct {
		Default protocol.ModelSelection `json:"default"`
	}
	if _, err := c.post(ctx, "session/modelCatalog", "modelCatalog", []byte(`{"args":{}}`), true, "", &cat); err != nil {
		return nil, fmt.Errorf("modelCatalog: %w", err)
	}
	var canOpen bool
	if _, err := c.post(ctx, "session/canOpenWorkspacePath", "canOpenWorkspacePath", []byte(`{"args":{}}`), true, "", &canOpen); err != nil {
		return nil, fmt.Errorf("canOpenWorkspacePath: %w", err)
	}
	var roster protocol.SessionListResponse
	if _, err := c.post(ctx, "session/list", "session.list", []byte(`{"args":{"_request":{}}}`), true, "", &roster); err != nil {
		return nil, fmt.Errorf("session.list: %w", err)
	}
	h := c.conn.Home()
	hd := &protocol.HostDescription{
		Provider:         cat.Default.Provider,
		Model:            cat.Default.Model,
		AttachedSessions: len(roster.Items),
		Home:             h,
		Cwd:              h,
		CanOpenPath:      canOpen,
	}
	// The new build publishes no version on the wire: the local dsh
	// launcher (the auto-start binary) answers for it.
	if hd.Version == "" {
		hd.Version = hostVersionFallback()
	}
	return hd, nil
}

// workspacesNew serves the registry from the workspace/follow mirror
// (the new build has no unary list): it waits for the first baseline
// the downlink delivers, bounded by the context.
func (c *Client) workspacesNew(ctx context.Context) (*protocol.WorkspaceListResponse, error) {
	waitCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	items, archived, ok := c.conn.WorkspaceBaseline(waitCtx)
	if !ok {
		return nil, fmt.Errorf("workspace.list: registry baseline pending (downlink not up)")
	}
	return &protocol.WorkspaceListResponse{Items: items, ArchivedSessionIds: archived}, nil
}
