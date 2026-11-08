package gates

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/lint"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// Gate run statuses.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusError   = "error"
	StatusSkipped = "skipped"
	StatusCached  = "cached"
	StatusMissing = "missing"
	StatusTimeout = "timeout"
	StatusFixed   = "fixed"
)

// Runner executes gates for one instance.
type Runner struct {
	Inst    *config.Instance
	Cfg     *Config
	Tel     *telemetry.Writer
	Now     func() time.Time
	Ratchet bool // shrink baselines when baselined findings disappear
	NoCache bool
}

// Request selects what to run.
type Request struct {
	Stage string
	Files []string // repo-relative slash paths
	Gates []string // optional gate-id filter
	Fix   bool     // run fix-mode gates' cmd (else their check_cmd, if any)
}

// GateResult is one gate's outcome.
type GateResult struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Findings  []finding.Finding `json:"findings"`
	Files     int               `json:"files"`
	Baselined int               `json:"baselined"`
	MS        int64             `json:"ms"`
	Blocking  int               `json:"blocking"`
}

// Report is the outcome of a stage run.
type Report struct {
	Stage     string            `json:"stage"`
	Gates     []GateResult      `json:"gates"`
	Blocking  []finding.Finding `json:"blocking"`
	Advisory  []finding.Finding `json:"advisory"`
	Baselined int               `json:"baselined"`
}

// Blocked reports whether any finding blocks.
func (r *Report) Blocked() bool { return len(r.Blocking) > 0 }

// Run executes the stage: fix-mode gates first (sequentially, they modify
// files), then check-mode gates in parallel bounded by CPU count. Results
// are collated deterministically by gate id.
func (r *Runner) Run(ctx context.Context, req Request) (*Report, error) {
	if !slices.Contains(Stages, req.Stage) {
		return nil, fmt.Errorf("unknown stage %q", req.Stage)
	}
	var fixes, checks []*Gate
	for i := range r.Cfg.Gates {
		g := &r.Cfg.Gates[i]
		if g.Disabled || !g.InStage(req.Stage) || (len(req.Gates) > 0 && !slices.Contains(req.Gates, g.ID)) {
			continue
		}
		if g.Mode == ModeFix {
			fixes = append(fixes, g)
		} else {
			checks = append(checks, g)
		}
	}
	slices.SortFunc(fixes, func(a, b *Gate) int { return cmp.Compare(a.ID, b.ID) })
	results := make([]GateResult, 0, len(fixes)+len(checks))
	for _, g := range fixes {
		results = append(results, r.runGate(ctx, g, req))
	}
	checkResults := make([]GateResult, len(checks))
	sem := make(chan struct{}, max(1, runtime.NumCPU()))
	var wg sync.WaitGroup
	for i, g := range checks {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			checkResults[i] = r.runGate(ctx, g, req)
		})
	}
	wg.Wait()
	results = append(results, checkResults...)
	return r.collate(req.Stage, results), nil
}

func (r *Runner) collate(stage string, results []GateResult) *Report {
	slices.SortStableFunc(results, func(a, b GateResult) int { return cmp.Compare(a.ID, b.ID) })
	rep := &Report{Stage: stage, Gates: results}
	for _, res := range results {
		g, _ := r.Cfg.Get(res.ID)
		rep.Baselined += res.Baselined
		for _, f := range res.Findings {
			if g.blocks(f) {
				rep.Blocking = append(rep.Blocking, f)
			} else {
				rep.Advisory = append(rep.Advisory, f)
			}
		}
	}
	sortBySeverity(rep.Blocking)
	sortBySeverity(rep.Advisory)
	return rep
}

func sortBySeverity(fs []finding.Finding) {
	finding.Sort(fs)
	slices.SortStableFunc(fs, func(a, b finding.Finding) int { return cmp.Compare(finding.Rank(a.Severity), finding.Rank(b.Severity)) })
}

// blocks reports whether f blocks: the gate is blocking and f's severity is
// error (or warning, for gates other than strata-lint).
func (g *Gate) blocks(f finding.Finding) bool {
	if !g.IsBlocking() || f.Rule == ruleToolMissing || f.Rule == ruleTimeout {
		return false
	}
	threshold := finding.Warning
	if g.Builtin == BuiltinLint {
		threshold = finding.Error
	}
	return finding.Rank(f.Severity) <= finding.Rank(threshold)
}

// Internal finding rules for gate infrastructure problems.
const (
	ruleToolMissing = "tool-missing"
	ruleTimeout     = "timeout"
	ruleCrashed     = "tool-failed"
)

// runGate runs one gate over the request's files.
func (r *Runner) runGate(ctx context.Context, g *Gate, req Request) GateResult {
	start := time.Now()
	files := g.Select(req.Files)
	res := GateResult{ID: g.ID, Files: len(files)}
	if len(files) == 0 {
		res.Status = StatusSkipped
		return res
	}
	var fs []finding.Finding
	if g.Module != "" {
		res.Status, fs = r.perModule(ctx, g, req, files)
	} else {
		res.Status, fs = r.executeIn(ctx, g, req, files)
	}
	for i := range fs {
		fs[i].Gate = g.ID
		if fs[i].Docs == "" && g.Docs != "" {
			fs[i].Docs = g.Docs
		}
	}
	fs, res.Baselined = r.baseline(g, files, fs)
	res.Findings = fs
	for _, f := range fs {
		if g.blocks(f) {
			res.Blocking++
		}
	}
	if res.Status == StatusPass && len(fs) > 0 {
		res.Status = StatusFail
	}
	res.MS = time.Since(start).Milliseconds()
	r.emit(g, req.Stage, res)
	return res
}

// builtin runs a built-in gate.
func (r *Runner) builtin(g *Gate, files []string) (string, []finding.Finding) {
	if g.Builtin == BuiltinSuppressions {
		return StatusPass, CheckSuppressions(r.Inst.Root, files)
	}
	rep, err := lint.Run(r.Inst, lint.Options{Paths: files})
	if err != nil {
		return StatusError, []finding.Finding{{Rule: ruleCrashed, Severity: finding.Error, Message: err.Error()}}
	}
	return StatusPass, rep.Findings
}

// command picks the argv template for a request: a fix gate without --fix
// runs its check_cmd (nil when it has none); the edit stage prefers edit_cmd.
func (g *Gate) command(req Request) []string {
	switch {
	case g.Mode == ModeFix && !req.Fix:
		if len(g.CheckCmd) == 0 {
			return nil
		}
		return g.CheckCmd
	case req.Stage == StageEdit && len(g.EditCmd) > 0:
		return g.EditCmd
	}
	return g.Cmd
}

// execute dispatches builtins, fix commands, cache lookups and tool runs.
func (r *Runner) execute(ctx context.Context, g *Gate, req Request, files []string) (string, []finding.Finding) {
	if g.Builtin != "" {
		return r.builtin(g, files)
	}
	cmd := g.command(req)
	if cmd == nil {
		return StatusSkipped, nil
	}
	fixing := g.Mode == ModeFix && req.Fix
	argv := Argv(cmd, g.Targets(files))
	var key string
	if !fixing && !r.NoCache {
		key = r.cacheKey(ctx, g, argv, files)
		if c, ok := r.cacheGet(key); ok {
			return StatusCached, c
		}
	}
	status, fs := r.invoke(ctx, g, argv)
	if fixing && status == StatusPass {
		status = StatusFixed
	}
	if key != "" && (status == StatusPass || status == StatusFail) {
		r.cachePut(key, fs)
	}
	return status, fs
}

// invoke runs the tool and parses its output.
func (r *Runner) invoke(ctx context.Context, g *Gate, argv []string) (string, []finding.Finding) {
	if _, err := exec.LookPath(argv[0]); err != nil {
		return StatusMissing, []finding.Finding{{
			Rule: ruleToolMissing, Severity: finding.Warning,
			Message: argv[0] + " not found on PATH; gate skipped", FixHint: "install it (strata doctor prints install commands)",
		}}
	}
	timeout, _ := config.ParseDuration(g.Timeout)
	cctx, cancel := context.WithTimeout(ctx, timeout.D())
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...) //nolint:gosec // G204: argv comes from the instance's own gates.toml
	cmd.Dir = filepath.Join(r.Inst.Root, filepath.FromSlash(g.Workdir))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if cctx.Err() != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return StatusTimeout, []finding.Finding{{Rule: ruleTimeout, Severity: finding.Warning, Message: "gate timed out after " + g.Timeout}}
	}
	out := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		out.Exit = ee.ExitCode()
	} else if err != nil {
		return StatusError, []finding.Finding{{Rule: ruleCrashed, Severity: finding.Error, Message: err.Error()}}
	}
	fs, perr := ParseOutput(g.Parser, out, g)
	// A non-zero exit that explains itself with no finding is a tool failure
	// (e.g. semgrep's exit 7 on a bad --config still prints valid, empty
	// SARIF): fail closed rather than report a pass.
	if out.Exit != 0 && (perr != nil || (len(fs) == 0 && g.Parser != ParserExitcode)) {
		o, _ := parseExitcode(out, g)
		o[0].Rule, o[0].Severity = ruleCrashed, finding.Error
		return StatusError, o
	}
	return StatusPass, fs
}

// normalize makes a tool-reported path repo-relative.
func (r *Runner) normalize(g *Gate, p string) string {
	if p == "" {
		return p
	}
	p = filepath.FromSlash(strings.TrimPrefix(p, "./"))
	if filepath.IsAbs(p) {
		for _, root := range []string{r.Inst.Root, realPath(r.Inst.Root)} {
			if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
		if rel, err := filepath.Rel(realPath(r.Inst.Root), realPath(p)); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
		return filepath.ToSlash(p)
	}
	if g.Workdir != "" {
		if _, err := os.Stat(filepath.Join(r.Inst.Root, p)); err != nil {
			return filepath.ToSlash(filepath.Join(g.Workdir, p))
		}
	}
	return filepath.ToSlash(filepath.Clean(p))
}

var moduleRE = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// goPackagePaths rewrites `go test -json` locations from import paths
// (module/pkg/file.go) to paths relative to the gate's workdir, using the
// module path in <workdir>/go.mod.
func (r *Runner) goPackagePaths(g *Gate, fs []finding.Finding) {
	raw, err := os.ReadFile(filepath.Join(r.Inst.Root, filepath.FromSlash(g.Workdir), "go.mod")) //nolint:gosec // G304: the gate's own go.mod
	if err != nil {
		return
	}
	m := moduleRE.FindSubmatch(raw)
	if m == nil {
		return
	}
	mod := string(m[1])
	for i := range fs {
		switch {
		case fs[i].File == mod:
			fs[i].File = "."
		case strings.HasPrefix(fs[i].File, mod+"/"):
			fs[i].File = strings.TrimPrefix(fs[i].File, mod+"/")
		}
	}
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// baseline separates baselined findings and ratchets the baseline down.
func (r *Runner) baseline(g *Gate, files []string, fs []finding.Finding) ([]finding.Finding, int) {
	if g.Baseline == "" {
		return fs, 0
	}
	bpath := r.Inst.Path(filepath.FromSlash(g.Baseline))
	b, err := LoadBaseline(bpath, g.ID)
	if err != nil {
		return append(fs, finding.Finding{Gate: g.ID, Rule: "baseline", Severity: finding.Warning, File: g.Baseline, Message: err.Error()}), 0
	}
	covered := coverage(g, files)
	fresh, n, stale := b.apply(fs, NewFingerprinter(r.Inst.Root), covered)
	if r.Ratchet && b.shrink(stale) {
		if err := b.Save(bpath); err != nil {
			fresh = append(fresh, finding.Finding{Gate: g.ID, Rule: "baseline", Severity: finding.Warning, File: g.Baseline, Message: err.Error()})
		}
	}
	return fresh, n
}

// coverage reports which files a run of g over files speaks for.
func coverage(g *Gate, files []string) func(string) bool {
	set := map[string]bool{}
	for _, f := range files {
		switch g.Scope {
		case ScopePackage:
			set[filepath.ToSlash(filepath.Dir(f))] = true
		default:
			set[f] = true
		}
	}
	return func(file string) bool {
		switch g.Scope {
		case ScopeRepo:
			return true
		case ScopePackage:
			return set[filepath.ToSlash(filepath.Dir(file))]
		default:
			return set[file]
		}
	}
}

func (r *Runner) emit(g *Gate, stage string, res GateResult) {
	if r.Tel == nil {
		return
	}
	counts := map[string]int{}
	for _, f := range res.Findings {
		counts[f.Rule]++
	}
	rules := make([]map[string]any, 0, len(counts))
	for _, id := range sortedKeys(counts) {
		rules = append(rules, map[string]any{"id": id, "count": counts[id]})
	}
	_ = r.Tel.Emit("gate", map[string]any{"gate": g.ID, "stage": stage, "files_n": res.Files, "result": res.Status, "rules": rules, "ms": res.MS})
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// executeIn runs the gate and makes finding paths repo-relative.
func (r *Runner) executeIn(ctx context.Context, g *Gate, req Request, files []string) (string, []finding.Finding) {
	status, fs := r.execute(ctx, g, req, files)
	if g.Parser == ParserGoTest {
		r.goPackagePaths(g, fs)
	}
	for i := range fs {
		fs[i].File = r.normalize(g, fs[i].File)
	}
	return status, fs
}

// perModule runs a module-aware gate once per nearest module root (the
// directory holding g.Module, e.g. go.mod), with that root as workdir.
// Files outside any module are skipped.
func (r *Runner) perModule(ctx context.Context, g *Gate, req Request, files []string) (string, []finding.Finding) {
	groups := map[string][]string{}
	for _, f := range files {
		if dir, ok := r.moduleRoot(path.Dir(f), g.Module); ok {
			groups[dir] = append(groups[dir], f)
		}
	}
	dirs := make([]string, 0, len(groups))
	for d := range groups {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)
	status := StatusSkipped
	var all []finding.Finding
	for _, d := range dirs {
		gc := *g
		gc.Workdir, gc.Module = d, ""
		st, fs := r.executeIn(ctx, &gc, req, groups[d])
		status = worseStatus(status, st)
		all = append(all, fs...)
	}
	return status, all
}

// moduleRoot finds the nearest ancestor of dir (repo-relative) containing
// file; "" denotes the repository root.
func (r *Runner) moduleRoot(dir, file string) (string, bool) {
	for {
		d := dir
		if d == "." {
			d = ""
		}
		if _, err := os.Stat(filepath.Join(r.Inst.Root, filepath.FromSlash(d), file)); err == nil {
			return d, true
		}
		if d == "" {
			return "", false
		}
		dir = path.Dir(dir)
	}
}

// statusRank orders statuses for merging per-module runs (higher = worse).
var statusRank = map[string]int{StatusSkipped: 0, StatusCached: 1, StatusPass: 2, StatusFixed: 3, StatusMissing: 4, StatusFail: 5, StatusTimeout: 6, StatusError: 7}

func worseStatus(a, b string) string {
	if statusRank[b] > statusRank[a] {
		return b
	}
	return a
}
