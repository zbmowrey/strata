package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/lint"
	"github.com/zbmowrey/strata/engine/internal/render"
)

func init() { register(newLintCmd) }

func newLintCmd(a *app) *cobra.Command {
	var fix bool
	var kinds []string
	cmd := &cobra.Command{
		Use:   "lint [paths…]",
		Short: "Validate knowledge, manifests, projects and links",
		RunE: func(_ *cobra.Command, args []string) error {
			format, err := a.formatOr("line", "line", "json", "jsonl")
			if err != nil {
				return err
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			opt := lint.Options{Fix: fix}
			for _, k := range kinds {
				if !validKind(k) {
					return fail.New(fail.Usage, "kinds: shard, hub, inbox, question, script, playbook, project", "unknown kind %q", k)
				}
				opt.Kinds = append(opt.Kinds, corpus.Kind(k))
			}
			if opt.Paths, err = relPaths(inst.Root, args); err != nil {
				return err
			}
			rep, err := lint.Run(inst, opt)
			if err != nil {
				return err
			}
			return a.printLint(rep, format)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "apply safe content-preserving fixes")
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "only lint these kinds")
	return cmd
}

func validKind(k string) bool {
	for _, x := range corpus.Kinds {
		if string(x) == k {
			return true
		}
	}
	return false
}

func (a *app) printLint(rep *lint.Report, format string) error {
	errs, warns := finding.Count(rep.Findings)
	switch format {
	case "json":
		if err := render.JSON(a.io.Out, map[string]any{"v": 1, "checked": rep.Checked, "errors": errs, "warnings": warns, "fixed": nonNil(rep.Fixed), "findings": nonNil(rep.Findings)}); err != nil {
			return err
		}
	case "jsonl":
		if err := render.JSONL(a.io.Out, rep.Findings); err != nil {
			return err
		}
	default:
		for _, f := range rep.Fixed {
			if _, err := fmt.Fprintln(a.io.Out, "fixed "+f); err != nil {
				return err
			}
		}
		if err := render.Findings(a.io.Out, rep.Findings); err != nil {
			return err
		}
		if !a.g.quiet {
			_, _ = fmt.Fprintf(a.io.Err, "strata lint: %s checked · %s · %s\n", render.Plural(rep.Checked, "file"), render.Plural(errs, "error"), render.Plural(warns, "warning"))
		}
	}
	if errs > 0 {
		return &fail.Error{Code: fail.Blocking, Err: fmt.Errorf("lint found %s", render.Plural(errs, "error")), Hint: "fix the findings above; `strata lint --fix` applies safe fixes"}
	}
	return nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
