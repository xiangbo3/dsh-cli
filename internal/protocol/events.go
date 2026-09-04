// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package protocol

import "encoding/json"

// UserMessageEventData is the data of a "user/message" event: the message
// itself (data == Message).
type UserMessageEventData = Message

// DecodeUserMessage decodes user/message data.
func DecodeUserMessage(data json.RawMessage) (*Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// DecodeAssistantMessage decodes assistant/message data.
func DecodeAssistantMessage(data json.RawMessage) (*AssistantMessageEventData, error) {
	var v AssistantMessageEventData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeChunk decodes assistant/chunk data.
func DecodeChunk(data json.RawMessage) (*ChunkEventData, error) {
	var v ChunkEventData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeToolCall decodes tool/call data.
func DecodeToolCall(data json.RawMessage) (*ToolCallEventData, error) {
	var v ToolCallEventData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeToolResult decodes tool/result data.
func DecodeToolResult(data json.RawMessage) (*ToolResultEventData, error) {
	var v ToolResultEventData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeTurnEnd decodes turn/end data.
func DecodeTurnEnd(data json.RawMessage) (*TurnEndEventData, error) {
	var v TurnEndEventData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeStep decodes step/start and step/end data.
func DecodeStep(data json.RawMessage) (*StepEventData, error) {
	var v StepEventData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeTodoWrite decodes todo/write data.
func DecodeTodoWrite(data json.RawMessage) (*TodoWriteEventData, error) {
	var v TodoWriteEventData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeTitle decodes session/title data.
func DecodeTitle(data json.RawMessage) (*TitleEventData, error) {
	var v TitleEventData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeRequestContext decodes request/context data.
func DecodeRequestContext(data json.RawMessage) (*RequestContextData, error) {
	var v RequestContextData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeGoalChange decodes goal/change data.
func DecodeGoalChange(data json.RawMessage) (*GoalChangeData, error) {
	var v GoalChangeData
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeGoalProjected decodes the "goal" projection value.
func DecodeGoalProjected(data json.RawMessage) (*GoalProjected, error) {
	var v GoalProjected
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DecodeListMetadata decodes the sessionListMetadata projection value
// (cold-session hints riding session.list rows).
type ListMetadata struct {
	Blank        bool  `json:"blank"`
	LastPromptAt int64 `json:"lastPromptAt"`
}

// SessionTextMessage extracts the concatenated text content of a message.
func SessionTextMessage(m *Message) string {
	var out []byte
	for _, b := range m.Content {
		if b.Type == "text" {
			if len(out) > 0 {
				out = append(out, '\n')
			}
			out = append(out, b.Text...)
		}
	}
	return string(out)
}

// ToolResultText extracts the readable text of a tool-result block list.
func ToolResultText(blocks []ContentBlock) string {
	var out []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, b.Text)
		case "tool-result":
			out = append(out, ToolResultText(b.Content))
		}
	}
	joined := ""
	for i, s := range out {
		if i > 0 {
			joined += "\n"
		}
		joined += s
	}
	return joined
}
