package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// Events are the supported hook events (`strata hook <event>`).
var Events = []string{"session-start", "pre-edit", "post-edit", "post-bash", "stop", "pre-compact"}

// GateResult is the outcome of running a gate stage.
type GateResult struct {
	Report   string // formatted findings (docs/spec/06 §3), empty when clean
	Hash     string // stable hash of the blocking finding set
	Rules    map[string]int
	Blocking int
}

// Gates runs a gate stage over paths (nil = the stage's default targets,
// e.g. changed files for stop). Wired by the CLI to internal/gates.
type Gates func(ctx context.Context, inst *config.Instance, stage string, paths []string) (GateResult, error)

// GardenAuto runs the deterministic gardener's safe subset at session start
// for the paths the index refresh found changed.
type GardenAuto func(ctx context.Context, inst *config.Instance, changed []string) (string, error)

// GardenDebt reports the session-start garden line ("" when nothing is due).
type GardenDebt func(inst *config.Instance, now time.Time) (string, error)

// Env is everything a hook needs besides its payload.
type Env struct {
	Now        func() time.Time
	Out        io.Writer
	Err        io.Writer
	Gates      Gates
	GardenAuto GardenAuto
	GardenDebt GardenDebt
	Root       string // instance root override (--root); else discovered from the payload cwd
	Subagent   bool
}

// outcome is what a handler decided.
type outcome struct {
	stdout string
	stderr string
	label  string // telemetry outcome
	code   int
}

// Run executes one hook and returns the process exit code. Internal errors
// never block: they exit 0 with a one-line warning on stderr.
func Run(ctx context.Context, event string, stdin io.Reader, env Env) (code int) {
	start := time.Now()
	if env.Now == nil {
		env.Now = time.Now
	}
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(env.Err, "strata hook %s: internal error: %v (fail-open)\n", event, r)
			code = 0
		}
	}()
	in, err := Decode(stdin)
	if err != nil {
		return warn(env, event, err)
	}
	inst, err := instanceFor(in, env)
	if err != nil {
		if fail.CodeOf(err) == fail.NotInstance {
			return 0 // hooks outside an instance are a no-op
		}
		return warn(env, event, err)
	}
	h := &handler{ctx: ctx, in: in, inst: inst, env: env, tel: telemetry.New(inst, env.Now).WithSession(in.SessionID)}
	out, err := h.dispatch(event)
	if err != nil {
		return warn(env, event, err)
	}
	if out.stdout != "" {
		_, _ = io.WriteString(env.Out, out.stdout)
	}
	if out.stderr != "" {
		_, _ = io.WriteString(env.Err, out.stderr)
	}
	_ = h.tel.Emit("hook", map[string]any{"hook": event, "ms": time.Since(start).Milliseconds(), "outcome": out.label})
	return out.code
}

func warn(env Env, event string, err error) int {
	_, _ = fmt.Fprintf(env.Err, "strata hook %s: %v (fail-open)\n", event, err)
	return 0
}

func instanceFor(in *Input, env Env) (*config.Instance, error) {
	if env.Root != "" {
		return config.Open(env.Root, "")
	}
	cwd := in.CWD
	if cwd == "" {
		return nil, fail.New(fail.NotInstance, "", "hook payload has no cwd")
	}
	return config.Open("", cwd)
}

type handler struct {
	ctx  context.Context
	in   *Input
	inst *config.Instance
	tel  *telemetry.Writer
	env  Env
}

func (h *handler) dispatch(event string) (outcome, error) {
	switch event {
	case "session-start":
		return h.sessionStart()
	case "pre-edit":
		return h.preEdit()
	case "post-edit":
		return h.postEdit()
	case "post-bash":
		return h.postBash()
	case "stop":
		return h.stop()
	case "pre-compact":
		return h.preCompact()
	default:
		return outcome{}, fail.New(fail.Usage, "events: "+strings.Join(Events, ", "), "unknown hook event %q", event)
	}
}

// rel converts an absolute path to an instance-relative slash path; ok is
// false for paths outside the instance. Symlinked roots (macOS /var →
// /private/var) are resolved on both sides.
func (h *handler) rel(abs string) (string, bool) {
	if abs == "" {
		return "", false
	}
	if r, ok := within(h.inst.Root, abs); ok {
		return r, true
	}
	root, err := filepath.EvalSymlinks(h.inst.Root)
	if err != nil {
		return "", false
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", false
	}
	return within(root, filepath.Join(dir, filepath.Base(abs)))
}

func within(base, p string) (string, bool) {
	r, err := filepath.Rel(base, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	r = filepath.ToSlash(r)
	// Nested worktrees (agent forks, garden runs) are separate checkouts:
	// this instance's guards and gates do not apply to them.
	for _, wt := range []string{".claude/worktrees/", ".strata/worktrees/"} {
		if strings.HasPrefix(r, wt) {
			return "", false
		}
	}
	return r, true
}

// blockJSON renders a Stop-style block decision.
func blockJSON(reason string) string {
	b, _ := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	return string(b) + "\n"
}

// systemMessage renders a message shown to the user (not the model).
func systemMessage(msg string) string {
	b, _ := json.Marshal(map[string]any{"systemMessage": msg})
	return string(b) + "\n"
}

// additionalContext renders context added for the model on events that
// support it (PreToolUse, PostToolUse, SessionStart).
func additionalContext(event, text string) string {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": event, "additionalContext": text}})
	return string(b) + "\n"
}
