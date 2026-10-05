// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package protocol

import "dsh-cli/internal/textutil"

// SessionTextMessage extracts the concatenated text content of a message,
// stripped of terminal escapes (pasted terminal output carries them).
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
	return textutil.StripTerminal(string(out))
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
	return textutil.StripTerminal(joined)
}
