package scripts

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// RunOptions configure one `strata run`.
type RunOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer // policy notices (hosts, pre-flight, warnings) and the script's stderr
	Tel    *telemetry.Writer
	Args   []string
	Yes    bool // confirms a destructive script
}

// RunResult is the outcome of a run.
type RunResult struct {
	Elapsed    time.Duration
	Violations []string // writes-local: changed paths outside the declared writes globs
	ExitCode   int
}

// ErrViolation marks a writes-local post-check failure.
var ErrViolation = errors.New("script wrote outside its declared writes globs")

// baseEnv names always passed through (docs/spec/11-security.md §4).
var baseEnv = []string{"PATH", "HOME", "USER", "LANG", "LANGUAGE", "TERM", "TMPDIR", "TZ", "GOPATH", "GOCACHE", "GOMODCACHE", "GOROOT", "GOFLAGS", "GOTOOLCHAIN", "VIRTUAL_ENV", "PYTHONPATH", "CARGO_HOME", "RUSTUP_HOME", "SYSTEMROOT"}

// envPrefixes are passed through by prefix.
var envPrefixes = []string{"LC_", "UV_"}

// FilterEnv keeps the base, prefixed and manifest-declared variables of env.
func FilterEnv(env, declared []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		keep := slices.Contains(baseEnv, name) || slices.Contains(declared, name)
		for _, p := range envPrefixes {
			keep = keep || strings.HasPrefix(name, p)
		}
		if keep {
			out = append(out, kv)
		}
	}
	return out
}

// Run executes a script under the side-effect policy (docs/spec/04 §4, §7).
func Run(ctx context.Context, inst *config.Instance, s *Script, opt RunOptions) (RunResult, error) {
	var res RunResult
	if err := preflight(s, opt); err != nil {
		return res, err
	}
	argv, extraEnv, err := command(ctx, inst, s, opt.Args)
	if err != nil {
		return res, err
	}
	var before map[string]bool
	tracked := s.SideEffect == WritesLocal
	if tracked {
		if before, err = gitStatus(ctx, inst.Root); err != nil {
			tracked = false
			_, _ = fmt.Fprintf(opt.Stderr, "strata: warning: cannot verify writes of %s (%v)\n", s.ID, err)
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...) //nolint:gosec // G204: explicit argv built from the manifest; never a shell
	cmd.Dir = inst.Root
	cmd.Env = append(FilterEnv(os.Environ(), s.Env), extraEnv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opt.Stdin, opt.Stdout, opt.Stderr
	cmd.WaitDelay = 2 * time.Second
	start := time.Now()
	runErr := cmd.Run()
	res.Elapsed = time.Since(start)
	res.ExitCode, err = exitCode(runCtx, s, runErr)
	if tracked && err == nil {
		res.Violations, err = checkWrites(ctx, inst.Root, s, before)
	}
	emit(opt.Tel, s, res)
	if err != nil {
		return res, err
	}
	if res.ExitCode != 0 {
		return res, &fail.Error{Code: res.ExitCode, Err: fmt.Errorf("script %s exited with status %d", s.ID, res.ExitCode)}
	}
	return res, nil
}

// preflight checks status, required tools and the side-effect policy.
func preflight(s *Script, opt RunOptions) error {
	if s.Status == "deprecated" {
		msg := "strata: warning: script " + s.ID + " is deprecated"
		if s.ReplacedBy != "" {
			msg += "; use " + s.ReplacedBy + " instead"
		}
		_, _ = fmt.Fprintln(opt.Stderr, msg)
	}
	for _, tool := range s.Requires {
		if _, err := exec.LookPath(tool); err != nil {
			return fail.New(fail.Runtime, "install "+tool+" and make sure it is on PATH", "script %s requires %q, which is not installed", s.ID, tool)
		}
	}
	switch s.SideEffect {
	case Network:
		_, _ = fmt.Fprintf(opt.Stderr, "strata: %s contacts: %s\n", s.ID, strings.Join(s.NetworkHosts, ", "))
	case Destructive:
		_, _ = fmt.Fprintf(opt.Stderr, "strata: %s is DESTRUCTIVE — %s\n  writes: %s\n  hosts: %s\n", s.ID, s.Summary, orNone(s.Writes), orNone(s.NetworkHosts))
		if !opt.Yes {
			return fail.New(fail.Usage, "re-run with --yes to confirm", "refusing to run destructive script %s without --yes", s.ID)
		}
	case ReadOnly, WritesLocal:
	default:
		return fail.New(fail.Runtime, "fix side_effect in script.yaml (strata lint)", "script %s has unknown side_effect %q", s.ID, s.SideEffect)
	}
	return nil
}

func orNone(v []string) string {
	if len(v) == 0 {
		return "(none declared)"
	}
	return strings.Join(v, ", ")
}

// command builds the argv (and extra environment) for the script's runtime.
func command(ctx context.Context, inst *config.Instance, s *Script, args []string) (argv, env []string, err error) {
	entry := filepath.Join(s.AbsDir(inst.Root), filepath.FromSlash(s.Entry))
	sd := scriptsDir(inst)
	switch s.Runtime {
	case RuntimeGo:
		b, err := Build(ctx, inst, s, false)
		if err != nil {
			return nil, nil, err
		}
		argv = []string{b.Binary}
	case RuntimePython:
		argv = []string{"uv", "run", "--quiet", "--project", sd, "python", entry}
		pp := filepath.Join(sd, "lib", "py")
		if cur := os.Getenv("PYTHONPATH"); cur != "" {
			pp += string(os.PathListSeparator) + cur
		}
		env = []string{"PYTHONPATH=" + pp}
	case "bash":
		argv = []string{"bash", entry}
	case "typescript":
		argv = []string{"pnpm", "--dir", filepath.Join(sd, "ts"), "exec", "tsx", entry}
	case "rust":
		argv = []string{"cargo", "run", "--quiet", "--manifest-path", filepath.Join(sd, "rust", "Cargo.toml"), "--bin", s.ID, "--"}
	default:
		return nil, nil, fail.New(fail.Runtime, "fix runtime in script.yaml (strata lint)", "script %s has unsupported runtime %q", s.ID, s.Runtime)
	}
	if s.Runtime != RuntimeGo && s.Runtime != "rust" {
		if _, err := os.Stat(entry); err != nil {
			return nil, nil, fail.New(fail.Runtime, "fix entry in script.yaml", "script %s entry %s not found", s.ID, s.Entry)
		}
		if _, err := exec.LookPath(argv[0]); err != nil {
			return nil, nil, fail.New(fail.Runtime, "install "+argv[0], "%s runtime needs %s on PATH", s.Runtime, argv[0])
		}
	}
	return append(argv, args...), env, nil
}

// exitCode maps the process outcome to an exit code or an error.
func exitCode(ctx context.Context, s *Script, err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return 1, fail.New(fail.Runtime, "raise timeout in script.yaml if the script legitimately needs longer", "script %s timed out after %s", s.ID, s.Timeout)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 1, fmt.Errorf("run %s: %w", s.ID, err)
}

// gitStatus returns the set of paths `git status --porcelain` reports.
func gitStatus(ctx context.Context, root string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	cmd.Dir = root
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git status: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	set := map[string]bool{}
	sc := bufio.NewScanner(&out)
	sc.Split(splitNUL)
	rename := false
	for sc.Scan() {
		rec := sc.Text()
		if rename { // the source path of a rename record
			set[rec] = true
			rename = false
			continue
		}
		if len(rec) < 4 {
			continue
		}
		set[rec[3:]] = true
		rename = rec[0] == 'R' || rec[0] == 'C'
	}
	return set, nil
}

func splitNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// checkWrites fails when paths that changed during the run fall outside the
// declared writes globs. Engine-owned state (.strata/) is ignored.
func checkWrites(ctx context.Context, root string, s *Script, before map[string]bool) ([]string, error) {
	after, err := gitStatus(ctx, root)
	if err != nil {
		return nil, err
	}
	var bad []string
	for p := range after {
		if before[p] || strings.HasPrefix(p, ".strata/") {
			continue
		}
		if !matchesAny(s.Writes, p) {
			bad = append(bad, p)
		}
	}
	slices.Sort(bad)
	if len(bad) > 0 {
		return bad, &fail.Error{
			Code: fail.Runtime, Hint: "declare the paths in writes: (a semantic change: new script or user approval) or fix the script",
			Err: fmt.Errorf("%w: %s wrote %s", ErrViolation, s.ID, strings.Join(bad, ", ")),
		}
	}
	return nil, nil
}

func matchesAny(globs []string, p string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, p); ok {
			return true
		}
		if ok, _ := doublestar.Match(strings.TrimSuffix(g, "/")+"/**", p); ok {
			return true
		}
	}
	return false
}

func emit(w *telemetry.Writer, s *Script, res RunResult) {
	fields := map[string]any{"id": s.ID, "exit": res.ExitCode, "ms": res.Elapsed.Milliseconds(), "side_effect": s.SideEffect}
	if len(res.Violations) > 0 {
		fields["violation"] = res.Violations
	}
	_ = w.Emit("script_run", fields)
}
