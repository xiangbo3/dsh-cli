package protocol

import (
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	env := Envelope{
		Type:    TypeClientRequest,
		RpcId:   "abc-123",
		Method:  "session.list",
		Payload: json.RawMessage(`{"cursor":""}`),
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeClientRequest || got.RpcId != "abc-123" || got.Method != "session.list" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeDownlinkMuxEvent(t *testing.T) {
	frame := `{"type":"server-request","rpcId":"r1","method":"session/event","payload":{"type":"session/event","sessionId":"s1","event":{"type":"turn/start","seq":7,"time":1,"data":{"turn":1}}}}`
	f, ok := DecodeDownlink([]byte(frame))
	if !ok {
		t.Fatal("decode failed")
	}
	if f.Kind != FMuxEvent || f.RpcId != "r1" {
		t.Fatalf("frame = %+v", f)
	}
	ev, err := DecodeMuxEvent(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev.SessionId != "s1" || ev.Event.Seq != 7 || ev.Event.Type != "turn/start" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestDecodeDownlinkHostFrame(t *testing.T) {
	frame := `{"type":"server-request","rpcId":"r2","method":"host/session-status","payload":{"type":"host/session-status","sessionId":"s9","running":true}}`
	f, ok := DecodeDownlink([]byte(frame))
	if !ok {
		t.Fatal("decode failed")
	}
	if f.Kind != FHostSessionStatus {
		t.Fatalf("kind = %q", f.Kind)
	}
	hf, err := DecodeHostFrame(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if hf.SessionId != "s9" || !hf.Running {
		t.Fatalf("frame = %+v", hf)
	}
}

func TestDecodeDownlinkQuadrantFilter(t *testing.T) {
	if _, ok := DecodeDownlink([]byte(`{"type":"server-response","rpcId":"x","result":{"ok":true}}`)); ok {
		t.Fatal("server-response should be filtered out")
	}
	if _, ok := DecodeDownlink([]byte(`not json`)); ok {
		t.Fatal("bad json should fail")
	}
}

func TestNewRPCId(t *testing.T) {
	a := NewRPCId()
	b := NewRPCId()
	if a == b {
		t.Fatal("ids should differ")
	}
	if len(a) != 36 {
		t.Fatalf("id = %q", a)
	}
}
