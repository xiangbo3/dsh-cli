package protocol

import "encoding/json"

// MuxFrame is one decoded downlink frame from either stream. Kind is the
// frame type discriminator (the payload's "type" field).
type MuxFrame struct {
	RpcId   string
	Kind    string
	Payload json.RawMessage
}

// DecodeDownlink parses one WS text message (a server-request envelope) into
// a frame. It returns ok=false for non-server-request quadrants or bad JSON.
func DecodeDownlink(data []byte) (MuxFrame, bool) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return MuxFrame{}, false
	}
	if env.Type != TypeServerRequest {
		return MuxFrame{}, false
	}
	var kind struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Payload, &kind); err != nil {
		return MuxFrame{RpcId: env.RpcId, Payload: env.Payload}, false
	}
	return MuxFrame{RpcId: env.RpcId, Kind: kind.Type, Payload: env.Payload}, true
}

// DecodeMuxEvent decodes a session/event frame.
func DecodeMuxEvent(p []byte) (*MuxSessionEvent, error) {
	var v MuxSessionEvent
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeMuxSubscribed decodes a session/subscribed frame.
func DecodeMuxSubscribed(p []byte) (*MuxSubscribed, error) {
	var v MuxSubscribed
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeMuxQueue decodes a session/queue frame.
func DecodeMuxQueue(p []byte) (*MuxQueue, error) {
	var v MuxQueue
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeMuxJobs decodes a session/jobs frame.
func DecodeMuxJobs(p []byte) (*MuxJobs, error) {
	var v MuxJobs
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeMuxProjection decodes a session/projection frame.
func DecodeMuxProjection(p []byte) (*MuxProjection, error) {
	var v MuxProjection
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeMuxApproval decodes an approval/requested frame.
func DecodeMuxApproval(p []byte) (*MuxApprovalRequested, error) {
	var v MuxApprovalRequested
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeMuxApprovalResolved decodes an approval/resolved frame.
func DecodeMuxApprovalResolved(p []byte) (*MuxApprovalResolved, error) {
	var v MuxApprovalResolved
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeMuxQuestion decodes a question/requested frame.
func DecodeMuxQuestion(p []byte) (*MuxQuestionRequested, error) {
	var v MuxQuestionRequested
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeMuxQuestionResolved decodes a question/resolved frame.
func DecodeMuxQuestionResolved(p []byte) (*MuxQuestionResolved, error) {
	var v MuxQuestionResolved
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeStreamError decodes a stream/error frame.
func DecodeStreamError(p []byte) (*StreamError, error) {
	var v StreamError
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeHostFrame decodes any host-stream frame (fields shared across kinds).
func DecodeHostFrame(p []byte) (*HostFrame, error) {
	var v HostFrame
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
