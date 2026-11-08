package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/bench"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/version"
)

func init() { benchSubcommands = append(benchSubcommands, newBenchProtocolCmd) }

// protocolClaude is replaced in tests (no live sessions in go test).
var protocolClaude bench.ClaudeFunc

type protocolFlags struct {
	suite     string
	model     string
	pluginDir string
	scenarios []string
	runs      int
	parallel  int
	list      bool
}

func newBenchProtocolCmd(a *app) *cobra.Command {
	var f protocolFlags
	cmd := &cobra.Command{
		Use:   "protocol",
		Short: "Run protocol evals: real headless Claude Code sessions scored over their tool calls",
		Long: "Scenarios come from the built-in suite (--suite standard) and the instance's .strata/evals/protocol/*.yaml.\n" +
			"Each scenario runs --runs times (default 3) and passes with ≥ 2/3. Results merge into .strata/bench/protocol.json.\n" +
			"Costs tokens: run on demand and before releases.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.runProtocol(cmd, f) },
	}
	fl := cmd.Flags()
	fl.StringArrayVar(&f.scenarios, "scenario", nil, "scenario id (repeatable; default: all)")
	fl.StringVar(&f.suite, "suite", bench.SuiteStandard, "built-in suite")
	fl.IntVar(&f.runs, "runs", 0, "runs per scenario (default: the scenario's, else 3)")
	fl.IntVar(&f.parallel, "parallel", 3, "sessions run at once")
	fl.StringVar(&f.model, "model", "", "model passed to claude --model")
	fl.StringVar(&f.pluginDir, "plugin-dir", "", "Strata plugin directory (default: found next to the binary or above the instance)")
	fl.BoolVar(&f.list, "list", false, "list the scenarios and exit")
	return cmd
}

func (a *app) runProtocol(cmd *cobra.Command, f protocolFlags) error {
	format, err := a.formatOr("line", "line", fmtJSON)
	if err != nil {
		return err
	}
	inst, err := a.instance()
	if err != nil {
		return err
	}
	all, err := bench.LoadSuite(f.suite)
	if err != nil {
		return err
	}
	own, err := bench.LoadInstanceScenarios(inst)
	if err != nil {
		return err
	}
	scenarios, err := bench.Select(append(all, own...), f.scenarios)
	if err != nil {
		return err
	}
	if f.list {
		for _, s := range scenarios {
			if err := a.printf("%-24s %s\n", s.ID, s.Description); err != nil {
				return err
			}
		}
		return nil
	}
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate strata binary: %w", err)
	}
	plugin := f.pluginDir
	if plugin == "" {
		plugin = findPluginDir(inst.Root, filepath.Dir(bin))
	}
	if plugin == "" {
		a.warnf("plugin directory not found; sessions run without the Strata plugin (pass --plugin-dir)")
	}
	rep := bench.RunProtocol(cmd.Context(), scenarios, bench.ProtocolOptions{
		Now: a.now, Claude: protocolClaude, Strata: bin, PluginDir: plugin, Model: f.model,
		Version: version.Version, Runs: f.runs, Parallel: f.parallel,
	})
	if _, err := bench.SaveProtocol(inst, rep); err != nil {
		return err
	}
	if err := a.printProtocol(rep, format); err != nil {
		return err
	}
	if rep.Passed < rep.Total {
		return fail.New(fail.Blocking, "see .strata/bench/protocol.json for the failed assertions", "%d of %s passed", rep.Passed, render.Plural(rep.Total, "scenario"))
	}
	return nil
}

// findPluginDir looks for plugin/.claude-plugin/plugin.json above each start.
func findPluginDir(starts ...string) string {
	for _, s := range starts {
		for dir := s; ; dir = filepath.Dir(dir) {
			p := filepath.Join(dir, "plugin")
			if _, err := os.Stat(filepath.Join(p, ".claude-plugin", "plugin.json")); err == nil {
				return p
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return ""
}

func (a *app) printProtocol(rep *bench.ProtocolReport, format string) error {
	if format == fmtJSON {
		return render.JSON(a.io.Out, rep)
	}
	for _, s := range rep.Scenarios {
		mark := "✓"
		if !s.Pass {
			mark = "✗"
		}
		if err := a.printf("%s %-24s %d/%d\n", mark, s.ID, s.Passed, s.Total); err != nil {
			return err
		}
		for i, r := range s.Runs {
			for _, msg := range r.Failures {
				if err := a.printf("    run %d: %s\n", i+1, render.OneLine(msg)); err != nil {
					return err
				}
			}
		}
	}
	return a.printf("%d/%d scenarios passed · model %s · $%.2f\n", rep.Passed, rep.Total, rep.Model, rep.CostUSD)
}
