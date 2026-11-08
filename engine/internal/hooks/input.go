// Package hooks implements the Claude Code hook adapters (`strata hook
// <event>`, docs/spec/08-claude-integration.md §4): stdin JSON → actions →
// stdout/stderr/exit. Internal errors fail open (exit 0 + one-line warning);
// only intended blocks exit 2.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

// MaxInput bounds hook stdin (docs/spec/11-security.md §5).
const MaxInput = 10 << 20

// Input is the hook payload; unknown fields are ignored.
type Input struct {
	ToolInput            json.RawMessage `json:"tool_input"`
	ToolResponse         json.RawMessage `json:"tool_response"`
	SessionID            string          `json:"session_id"`
	TranscriptPath       string          `json:"transcript_path"`
	CWD                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	ToolName             string          `json:"tool_name"`
	ToolUseID            string          `json:"tool_use_id"`
	AgentID              string          `json:"agent_id"`
	AgentType            string          `json:"agent_type"`
	Source               string          `json:"source"`
	Trigger              string          `json:"trigger"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	StopHookActive       bool            `json:"stop_hook_active"`
}

// EditInput is the subset of Edit/Write/MultiEdit/NotebookEdit tool_input
// Strata reads.
type EditInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	OldString    string `json:"old_string"`
	Content      string `json:"content"`
	Edits        []struct {
		OldString string `json:"old_string"`
	} `json:"edits"`
}

// BashInput is the Bash tool_input.
type BashInput struct {
	Command string `json:"command"`
}

// ErrTooLarge is returned for payloads over MaxInput.
var ErrTooLarge = errors.New("hook input exceeds 10 MB")

// Decode reads and parses a hook payload.
func Decode(r io.Reader) (*Input, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxInput+1))
	if err != nil {
		return nil, fmt.Errorf("read hook input: %w", err)
	}
	if len(raw) > MaxInput {
		return nil, ErrTooLarge
	}
	var in Input
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("parse hook input: %w", err)
	}
	return &in, nil
}

// EditPath returns the absolute path an edit tool targets ("" if none).
func (in *Input) EditPath() string {
	var e EditInput
	if json.Unmarshal(in.ToolInput, &e) != nil {
		return ""
	}
	p := e.FilePath
	if p == "" {
		p = e.NotebookPath
	}
	if p != "" && !filepath.IsAbs(p) && in.CWD != "" {
		p = filepath.Join(in.CWD, p)
	}
	return p
}

// Edit returns the decoded edit input.
func (in *Input) Edit() EditInput {
	var e EditInput
	_ = json.Unmarshal(in.ToolInput, &e)
	return e
}

// BashCommand returns the Bash command ("" if none).
func (in *Input) BashCommand() string {
	var b BashInput
	_ = json.Unmarshal(in.ToolInput, &b)
	return b.Command
}
