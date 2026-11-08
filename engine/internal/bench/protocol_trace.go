package bench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

// Call is one tool use in a session (main loop or subagent).
type Call struct {
	Input map[string]any
	Name  string
	Sub   bool // issued by a subagent
}

// Trace is what a headless session did, parsed from stream-json.
type Trace struct {
	Model   string
	Answer  string
	Result  string // result subtype (success, error_max_turns, …)
	Calls   []Call
	CostUSD float64
	Turns   int
	IsError bool
}

type streamEvent struct {
	Type    string  `json:"type"`
	Subtype string  `json:"subtype"`
	Model   string  `json:"model"`
	Result  string  `json:"result"`
	Parent  *string `json:"parent_tool_use_id"`
	Message struct {
		Model   string `json:"model"`
		Content []struct {
			Input map[string]any `json:"input"`
			Type  string         `json:"type"`
			Name  string         `json:"name"`
			Text  string         `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Cost    float64 `json:"total_cost_usd"`
	Turns   int     `json:"num_turns"`
	IsError bool    `json:"is_error"`
}

// ParseTrace decodes a stream-json transcript; malformed lines are skipped.
func ParseTrace(raw []byte) *Trace {
	t := &Trace{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	lastText := ""
	for sc.Scan() {
		var ev streamEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "system":
			if ev.Subtype == "init" && ev.Model != "" {
				t.Model = ev.Model
			}
		case "assistant":
			if text := t.addAssistant(&ev); text != "" {
				lastText = text
			}
		case "result":
			t.Answer, t.Result, t.CostUSD, t.Turns, t.IsError = ev.Result, ev.Subtype, ev.Cost, ev.Turns, ev.IsError
		}
	}
	if t.Answer == "" {
		t.Answer = lastText
	}
	return t
}

// addAssistant records an assistant message's tool calls and returns its
// last main-loop text.
func (t *Trace) addAssistant(ev *streamEvent) string {
	sub := ev.Parent != nil && *ev.Parent != ""
	if t.Model == "" && ev.Message.Model != "" && !sub {
		t.Model = ev.Message.Model
	}
	text := ""
	for _, c := range ev.Message.Content {
		switch {
		case c.Type == "tool_use":
			t.Calls = append(t.Calls, Call{Name: c.Name, Input: c.Input, Sub: sub})
		case c.Type == "text" && !sub:
			text = c.Text
		}
	}
	return text
}

// toolPattern is `Name` or `Name(arg glob)`.
type toolPattern struct {
	arg  *regexp.Regexp
	name string
}

func parsePattern(p string) toolPattern {
	name, arg, ok := strings.Cut(p, "(")
	tp := toolPattern{name: strings.TrimSpace(name)}
	if ok {
		tp.arg = globRegexp(strings.TrimSuffix(arg, ")"))
	}
	return tp
}

// globRegexp turns a `*`/`?` glob into an anchored regular expression.
func globRegexp(g string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("(?s)^")
	for _, r := range g {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// nameMatches compares tool names; a plugin model tool (`strata_search`)
// also matches its MCP name (`mcp__strata__strata_search`).
func nameMatches(want, got string) bool {
	return want == got || (strings.HasPrefix(want, "strata_") && strings.HasSuffix(got, "__"+want))
}

func (tp toolPattern) matches(c Call) bool {
	if !nameMatches(tp.name, c.Name) {
		return false
	}
	if tp.arg == nil {
		return true
	}
	for _, a := range callArgs(c) {
		if tp.arg.MatchString(a) {
			return true
		}
	}
	return false
}

var reShellSplit = regexp.MustCompile(`&&|\|\||[;|\n]`)

// callArgs returns the strings a pattern's argument glob is matched
// against: each simple command of a Bash line (a path to the strata binary
// normalized to `strata`), else the tool's primary input.
func callArgs(c Call) []string {
	str := func(k string) string { s, _ := c.Input[k].(string); return s }
	switch c.Name {
	case "Bash":
		var out []string
		for _, seg := range reShellSplit.Split(str("command"), -1) {
			seg = strings.TrimSpace(seg)
			if f := strings.Fields(seg); len(f) > 0 && filepath.Base(f[0]) == "strata" {
				seg = "strata" + strings.TrimPrefix(seg, f[0])
			}
			if seg != "" {
				out = append(out, seg)
			}
		}
		return out
	case "Read", "Write", "Edit", "MultiEdit":
		return []string{str("file_path")}
	case "NotebookEdit":
		return []string{str("notebook_path")}
	case "Grep", "Glob":
		return []string{str("pattern")}
	}
	if q := str("query"); q != "" {
		return []string{q}
	}
	b, _ := json.Marshal(c.Input)
	return []string{string(b)}
}

// count returns how many calls match any of the patterns, and the index of
// the first one (-1 when none).
func (t *Trace) count(patterns []string) (n, first int) {
	ps := make([]toolPattern, 0, len(patterns))
	for _, p := range patterns {
		ps = append(ps, parsePattern(p))
	}
	first = -1
	for i, c := range t.Calls {
		for _, p := range ps {
			if p.matches(c) {
				n++
				if first < 0 {
					first = i
				}
				break
			}
		}
	}
	return n, first
}
