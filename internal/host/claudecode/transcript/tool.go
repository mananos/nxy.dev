package transcript

import (
	"bytes"
	"encoding/json"
	"strings"
)

// maxArgRunes is the longest one-line argument LastToolUse returns.
const maxArgRunes = 60

type textLine struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// completeLines splits data into lines, dropping the last one when the data does not end in a
// newline (a line still being written).
func completeLines(data []byte) [][]byte {
	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// textOf is the text of a message content: a plain string, or the text blocks joined by newlines.
func textOf(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil && s != "" {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// FirstUserText is the text of the first user message of data (the prompt a subagent was given).
// Lines that do not parse are skipped, and so is a trailing line without its newline when it is cut.
func FirstUserText(data []byte) string {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var e textLine
		if json.Unmarshal(line, &e) != nil || e.Type != "user" {
			continue
		}
		if s := textOf(e.Message.Content); s != "" {
			return s
		}
	}
	return ""
}

// LastAssistantText is the text of the last assistant message of data that has text (the final
// report of a subagent). An incomplete trailing line is ignored.
func LastAssistantText(data []byte) string {
	lines := completeLines(data)
	for i := len(lines) - 1; i >= 0; i-- {
		var e textLine
		if json.Unmarshal(lines[i], &e) != nil || e.Type != "assistant" {
			continue
		}
		if s := textOf(e.Message.Content); s != "" {
			return s
		}
	}
	return ""
}

// LastToolUse is the last tool_use block of the complete assistant lines of data: the tool name and
// a short one-line argument. An incomplete trailing line is ignored.
func LastToolUse(data []byte) (name, arg string, ok bool) {
	lines := completeLines(data)
	for i := len(lines) - 1; i >= 0; i-- {
		var e textLine
		if json.Unmarshal(lines[i], &e) != nil || e.Type != "assistant" {
			continue
		}
		var blocks []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			continue
		}
		for j := len(blocks) - 1; j >= 0; j-- {
			if blocks[j].Type == "tool_use" && blocks[j].Name != "" {
				return blocks[j].Name, toolArg(blocks[j].Input), true
			}
		}
	}
	return "", "", false
}

func toolArg(input json.RawMessage) string {
	var in struct {
		FilePath    string `json:"file_path"`
		Path        string `json:"path"`
		Command     string `json:"command"`
		Pattern     string `json:"pattern"`
		Description string `json:"description"`
	}
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	switch {
	case in.FilePath != "":
		return baseName(in.FilePath)
	case in.Path != "":
		return baseName(in.Path)
	case in.Command != "":
		return oneLine(in.Command, maxArgRunes)
	case in.Pattern != "":
		return oneLine(in.Pattern, maxArgRunes)
	case in.Description != "":
		return oneLine(in.Description, maxArgRunes)
	}
	return ""
}

// baseName is the last element of a path with either separator, so a Windows path reads the same on Linux.
func baseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// oneLine is the first line of s cut to max runes.
func oneLine(s string, max int) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}
