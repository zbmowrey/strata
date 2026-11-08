package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/setup"
)

// DefaultAllowedTools is what eval sessions may use without prompting
// (temp workspaces are untrusted, so settings.json allowlists are ignored).
var DefaultAllowedTools = []string{
	"Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "Agent", "Skill",
	"Bash(strata:*)", "Bash(git:*)", "Bash(ls:*)", "Bash(cat:*)", "Bash(go:*)", "Bash(uv:*)",
	"Bash(python3:*)", "Bash(pytest:*)", "Bash(grep:*)", "Bash(find:*)",
	"mcp__strata__strata_search", "mcp__strata__strata_show",
}

// DefaultRuns and PassThreshold: a scenario passes with ≥ 2 of 3 runs.
const (
	DefaultRuns     = 3
	DefaultTimeout  = 10 * time.Minute
	DefaultMaxTurns = 30
)

// ClaudeFunc runs one headless session in dir and returns its stream-json.
type ClaudeFunc func(ctx context.Context, dir string, args, env []string) ([]byte, error)

// ProtocolOptions configure a protocol run.
type ProtocolOptions struct {
	Now       func() time.Time
	Claude    ClaudeFunc                                           // default: exec `claude`
	Gate      func(ctx context.Context, dir string) (bool, string) // default: `strata check --all`
	Strata    string                                               // strata binary (put on PATH for hooks)
	PluginDir string                                               // passed as --plugin-dir when set
	Model     string
	Version   string
	Runs      int // overrides each scenario's runs when > 0
	Parallel  int
}

// RunResult is one session's outcome.
type RunResult struct {
	Failures []string `json:"failures"`
	Model    string   `json:"model,omitempty"`
	CostUSD  float64  `json:"cost_usd"`
	Turns    int      `json:"turns"`
	MS       int64    `json:"ms"`
	Pass     bool     `json:"pass"`
}

// ScenarioResult aggregates a scenario's runs.
type ScenarioResult struct {
	ID     string      `json:"id"`
	Date   string      `json:"date"`
	Model  string      `json:"model,omitempty"`
	Runs   []RunResult `json:"runs"`
	Passed int         `json:"passed"`
	Total  int         `json:"total"`
	Pass   bool        `json:"pass"`
}

// ProtocolReport is `.strata/bench/protocol.json`.
type ProtocolReport struct {
	Date      string           `json:"date"`
	Model     string           `json:"model,omitempty"`
	Scenarios []ScenarioResult `json:"scenarios"`
	CostUSD   float64          `json:"cost_usd"`
	Passed    int              `json:"passed"`
	Total     int              `json:"total"`
	V         int              `json:"v"`
}

type job struct {
	sc     *Scenario
	si, ri int
}

// RunProtocol runs every scenario's sessions (Parallel at a time) and
// scores them; a scenario passes when at least two thirds of its runs do.
func RunProtocol(ctx context.Context, scenarios []Scenario, opt ProtocolOptions) *ProtocolReport {
	opt = opt.withDefaults()
	results := make([][]RunResult, len(scenarios))
	var jobs []job
	for i := range scenarios {
		n := DefaultRuns
		if scenarios[i].Runs > 0 {
			n = scenarios[i].Runs
		}
		if opt.Runs > 0 {
			n = opt.Runs
		}
		results[i] = make([]RunResult, n)
		for r := range n {
			jobs = append(jobs, job{sc: &scenarios[i], si: i, ri: r})
		}
	}
	var wg sync.WaitGroup
	next := make(chan job)
	for range max(1, opt.Parallel) {
		wg.Go(func() {
			for j := range next {
				results[j.si][j.ri] = runOnce(ctx, j.sc, opt)
			}
		})
	}
	for _, j := range jobs {
		next <- j
	}
	close(next)
	wg.Wait()
	return report(scenarios, results, opt.Now())
}

func (o ProtocolOptions) withDefaults() ProtocolOptions {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Claude == nil {
		o.Claude = execClaude
	}
	if o.Gate == nil {
		bin := o.Strata
		o.Gate = func(ctx context.Context, dir string) (bool, string) { return strataCheck(ctx, bin, dir) }
	}
	if o.Parallel <= 0 {
		o.Parallel = 3
	}
	return o
}

func report(scenarios []Scenario, results [][]RunResult, now time.Time) *ProtocolReport {
	rep := &ProtocolReport{V: 1, Date: now.Format(time.DateOnly)}
	for i, sc := range scenarios {
		sr := ScenarioResult{ID: sc.ID, Date: rep.Date, Runs: results[i], Total: len(results[i])}
		for _, r := range results[i] {
			if r.Pass {
				sr.Passed++
			}
			if r.Model != "" {
				sr.Model = r.Model
			}
			rep.CostUSD += r.CostUSD
		}
		sr.Pass = sr.Total > 0 && 3*sr.Passed >= 2*sr.Total
		if sr.Model != "" {
			rep.Model = sr.Model
		}
		rep.Scenarios = append(rep.Scenarios, sr)
	}
	rep.finish()
	return rep
}

func (rep *ProtocolReport) finish() {
	rep.Passed, rep.Total = 0, len(rep.Scenarios)
	for _, s := range rep.Scenarios {
		if s.Pass {
			rep.Passed++
		}
	}
	rep.CostUSD = float64(int64(rep.CostUSD*10000+0.5)) / 10000
}

// runOnce prepares a fresh fixture copy, runs the session and checks it.
func runOnce(ctx context.Context, sc *Scenario, opt ProtocolOptions) RunResult {
	start := time.Now()
	dir, err := os.MkdirTemp("", "strata-eval-"+sc.ID+"-*")
	if err != nil {
		return RunResult{Failures: []string{err.Error()}}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	res := RunResult{Failures: []string{}}
	before, err := prepare(ctx, sc, dir, opt)
	if err != nil {
		res.Failures = append(res.Failures, "setup: "+err.Error())
		return res
	}
	timeout := DefaultTimeout
	if d, err := time.ParseDuration(sc.Budget.Timeout); err == nil && d > 0 {
		timeout = d
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, runErr := opt.Claude(rctx, dir, claudeArgs(sc, opt), sessionEnv(opt.Strata))
	trace := ParseTrace(out)
	res.Model, res.CostUSD, res.Turns, res.MS = trace.Model, trace.CostUSD, trace.Turns, time.Since(start).Milliseconds()
	res.Failures = append(res.Failures, budgetFailures(sc, trace, runErr)...)
	after, err := takeSnapshot(dir)
	if err != nil {
		res.Failures = append(res.Failures, err.Error())
		return res
	}
	rs := &runState{trace: trace, before: before, after: after, dir: dir, gate: func() (bool, string) { return opt.Gate(ctx, dir) }}
	res.Failures = append(res.Failures, rs.evaluate(sc.Assert)...)
	res.Pass = len(res.Failures) == 0
	return res
}

func budgetFailures(sc *Scenario, t *Trace, runErr error) []string {
	var out []string
	switch {
	case errors.Is(runErr, context.DeadlineExceeded):
		out = append(out, "session timed out")
	case runErr != nil && t.Result == "":
		out = append(out, "session failed: "+runErr.Error())
	}
	if strings.HasPrefix(t.Result, "error") {
		out = append(out, "session ended with "+t.Result)
	}
	if sc.Budget.MaxCostUSD > 0 && t.CostUSD > sc.Budget.MaxCostUSD {
		out = append(out, fmt.Sprintf("cost $%.2f > budget $%.2f", t.CostUSD, sc.Budget.MaxCostUSD))
	}
	return out
}

func claudeArgs(sc *Scenario, opt ProtocolOptions) []string {
	tools := sc.AllowedTools
	if len(tools) == 0 {
		tools = DefaultAllowedTools
	}
	turns := sc.Budget.MaxTurns
	if turns <= 0 {
		turns = DefaultMaxTurns
	}
	args := []string{
		"-p", sc.Prompt, "--output-format", "stream-json", "--verbose",
		"--permission-mode", "acceptEdits", "--allowedTools", strings.Join(tools, ","), "--max-turns", strconv.Itoa(turns),
	}
	if opt.PluginDir != "" {
		args = append(args, "--plugin-dir", opt.PluginDir)
	}
	if opt.Model != "" {
		args = append(args, "--model", opt.Model)
	}
	return args
}

// sessionEnv puts the strata binary first on PATH (hooks call `strata`).
func sessionEnv(strata string) []string {
	env := os.Environ()
	if strata == "" {
		return env
	}
	dir := filepath.Dir(strata)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
			return env
		}
	}
	return append(env, "PATH="+dir)
}

// prepare copies the fixture, writes scenario files, makes a git repo,
// runs `strata init` and the setup steps, commits, and snapshots.
func prepare(ctx context.Context, sc *Scenario, dir string, opt ProtocolOptions) (snapshot, error) {
	src, err := sc.fixtureFS()
	if err != nil {
		return nil, err
	}
	if err := copyFixture(src, dir); err != nil {
		return nil, err
	}
	if err := writeFiles(dir, sc.Files); err != nil {
		return nil, err
	}
	env := append(sessionEnv(opt.Strata), "GIT_AUTHOR_NAME=strata-eval", "GIT_AUTHOR_EMAIL=eval@strata.invalid",
		"GIT_COMMITTER_NAME=strata-eval", "GIT_COMMITTER_EMAIL=eval@strata.invalid")
	if err := run(ctx, dir, env, "git", "init", "--quiet", "-b", "main"); err != nil {
		return nil, err
	}
	if _, err := setup.Init(ctx, dir, setup.Options{Now: opt.Now, Version: opt.Version}); err != nil {
		return nil, fmt.Errorf("strata init: %w", err)
	}
	for _, step := range sc.Setup {
		// Setup steps are shell by contract (docs/spec/10-evals.md §2): the
		// eval author's own commands, run in a throwaway fixture copy.
		if err := run(ctx, dir, env, "sh", "-c", step); err != nil {
			return nil, err
		}
	}
	if err := run(ctx, dir, env, "git", "add", "-A"); err != nil {
		return nil, err
	}
	if err := run(ctx, dir, env, "git", "commit", "--quiet", "--no-verify", "-m", "eval fixture"); err != nil {
		return nil, err
	}
	return takeSnapshot(dir)
}

func run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: git, or a scenario's setup step (see prepare)
	cmd.Dir, cmd.Env = dir, env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, firstLines(string(out), 3))
	}
	return nil
}

func execClaude(ctx context.Context, dir string, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "claude", args...) //nolint:gosec // G204: fixed program; args built by the runner
	cmd.Dir, cmd.Env = dir, env
	cmd.WaitDelay = 10 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("claude: %w: %s", err, firstLines(stderr.String(), 3))
	}
	return out, nil
}

func strataCheck(ctx context.Context, bin, dir string) (bool, string) {
	if bin == "" {
		bin = "strata"
	}
	cmd := exec.CommandContext(ctx, bin, "--root", dir, "check", "--all") //nolint:gosec // G204: our own binary
	cmd.Env = sessionEnv(bin)
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

// SaveProtocol merges rep into the instance's protocol.json by scenario id.
func SaveProtocol(inst *config.Instance, rep *ProtocolReport) (*ProtocolReport, error) {
	p := inst.Path(filepath.FromSlash(ProtocolResult))
	merged := &ProtocolReport{V: 1}
	if raw, err := os.ReadFile(p); err == nil { //nolint:gosec // G304: instance bench file
		_ = json.Unmarshal(raw, merged)
	}
	for _, s := range rep.Scenarios {
		i := slices.IndexFunc(merged.Scenarios, func(x ScenarioResult) bool { return x.ID == s.ID })
		if i >= 0 {
			merged.Scenarios[i] = s
		} else {
			merged.Scenarios = append(merged.Scenarios, s)
		}
	}
	slices.SortFunc(merged.Scenarios, func(a, b ScenarioResult) int { return strings.Compare(a.ID, b.ID) })
	merged.V, merged.Date, merged.CostUSD = 1, rep.Date, rep.CostUSD
	if rep.Model != "" {
		merged.Model = rep.Model
	}
	merged.finish()
	b, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("protocol results: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return nil, fmt.Errorf("protocol results: %w", err)
	}
	return merged, fsutil.WriteAtomic(p, append(b, '\n'), 0o644)
}
