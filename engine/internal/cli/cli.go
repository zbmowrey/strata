// Package cli wires the cobra command tree. It parses arguments and renders
// results; all logic lives in the domain packages.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/fail"
)

// Streams are the process's standard streams.
type Streams struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	OutFd  uintptr
	OutTTY bool
}

// OSStreams returns the real standard streams.
func OSStreams() Streams {
	return Streams{
		In:     os.Stdin,
		Out:    os.Stdout,
		Err:    os.Stderr,
		OutFd:  os.Stdout.Fd(),
		OutTTY: isatty.IsTerminal(os.Stdout.Fd()),
	}
}

// globals holds the global flags of one invocation.
type globals struct {
	root        string
	format      string
	noColor     bool
	noRefresh   bool
	strictFresh bool
	quiet       bool
	verbose     bool
}

// app is the per-invocation context shared by commands.
type app struct {
	io Streams
	g  globals
}

// Output format names.
const (
	fmtJSON  = "json"
	fmtJSONL = "jsonl"
)

// NowEnv overrides the clock (YYYY-MM-DD or RFC 3339) for reproducible
// output in tests, evals and golden files.
const NowEnv = "STRATA_NOW"

// now returns the current time, honoring STRATA_NOW.
func (a *app) now() time.Time {
	if v := os.Getenv(NowEnv); v != "" {
		for _, layout := range []string{time.RFC3339, time.DateOnly} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

// Main runs the CLI with args and returns the process exit code.
func Main(args []string, s Streams) (code int) {
	a := &app{io: s}
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(s.Err, "strata: internal error: %v (hint: this is a bug — please report it with the command you ran)\n", r)
			code = fail.Runtime
		}
	}()
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetIn(s.In)
	root.SetOut(s.Out)
	root.SetErr(s.Err)
	err := root.Execute()
	if err == nil {
		return fail.OK
	}
	err = classify(err)
	a.reportError(err)
	return fail.CodeOf(err)
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "strata",
		Short:         "Strata — a drop-in operating layer for Claude Code",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fail.Wrap(fail.Usage, "see --help", err)
	})
	f := root.PersistentFlags()
	f.StringVar(&a.g.root, "root", "", "instance root (default: discovered)")
	f.StringVar(&a.g.format, "format", "", "output format: line|table|json|jsonl|md")
	f.BoolVar(&a.g.noColor, "no-color", false, "disable ANSI color")
	f.BoolVar(&a.g.noRefresh, "no-refresh", false, "skip index freshness check")
	f.BoolVar(&a.g.strictFresh, "strict-fresh", false, "wait for the index lock")
	f.BoolVarP(&a.g.quiet, "quiet", "q", false, "suppress non-essential stderr")
	f.BoolVarP(&a.g.verbose, "verbose", "v", false, "debug logging to stderr")
	for _, add := range commandSet {
		root.AddCommand(add(a))
	}
	return root
}

// commandSet lists constructors for the top-level commands; each command file
// appends to it from an init function, keeping this file free of churn.
var commandSet []func(*app) *cobra.Command

func register(f func(*app) *cobra.Command) { commandSet = append(commandSet, f) }

// cobraUsagePrefixes identify argument errors cobra reports as plain errors.
var cobraUsagePrefixes = []string{"unknown command", "required flag", "unknown flag", "unknown shorthand", "accepts ", "requires at least", "requires at most", "invalid argument", "flag needs"}

// classify maps cobra's untyped usage errors to fail.Usage.
func classify(err error) error {
	if fail.CodeOf(err) != fail.Runtime {
		return err
	}
	msg := err.Error()
	for _, p := range cobraUsagePrefixes {
		if strings.HasPrefix(msg, p) {
			return fail.Wrap(fail.Usage, "see strata --help", err)
		}
	}
	return err
}

// reportError prints err in the contract format:
// `strata: <what failed>: <why> (hint: <fix>)`.
func (a *app) reportError(err error) {
	msg := err.Error()
	hint := fail.HintOf(err)
	line := "strata: " + msg
	if hint != "" {
		line += " (hint: " + hint + ")"
	}
	_, _ = fmt.Fprintln(a.io.Err, line)
	if a.g.format == fmtJSON {
		payload := map[string]any{"v": 1, "error": map[string]any{"message": msg, "hint": hint, "code": fail.CodeOf(err)}}
		_ = json.NewEncoder(a.io.Out).Encode(payload)
	}
}

// formatOr returns the --format value or def when unset, validating it
// against allowed.
func (a *app) formatOr(def string, allowed ...string) (string, error) {
	f := a.g.format
	if f == "" {
		return def, nil
	}
	for _, x := range allowed {
		if f == x {
			return f, nil
		}
	}
	return "", fail.New(fail.Usage, "use one of: "+strings.Join(allowed, ", "), "unsupported --format %q", f)
}
