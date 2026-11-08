package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/scripts"
)

func init() {
	register(newRunCmd)
	register(newScriptsCmd)
	registerNew(newScriptCmd)
}

func newScriptCmd(a *app) *cobra.Command {
	var runtime string
	cmd := &cobra.Command{
		Use:   "script <domain>/<name>",
		Short: "Scaffold a script: manifest (TODOs fail lint until filled), entry point, failing test",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			domain, name, ok := strings.Cut(args[0], "/")
			if !ok || strings.Contains(name, "/") {
				return fail.New(fail.Usage, "use strata new script <domain>/<name>", "bad script path %q", args[0])
			}
			created, err := scripts.Scaffold(cmd.Context(), inst, scripts.NewOptions{Domain: domain, Name: name, Runtime: runtime})
			for _, p := range created {
				if perr := a.printf("%s\n", p); perr != nil {
					return perr
				}
			}
			return err
		},
	}
	cmd.Flags().StringVar(&runtime, "runtime", "", "go | python (default from [scripts] default_runtime)")
	return cmd
}

func newRunCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "run <id> [--yes] [-- args…]",
		Short: "Run a catalogued script under its side-effect policy",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			s, err := scripts.Find(inst, args[0])
			if err != nil {
				return err
			}
			_, err = scripts.Run(cmd.Context(), inst, s, scripts.RunOptions{
				Args: args[1:], Yes: yes, Stdin: a.io.In, Stdout: a.io.Out, Stderr: a.io.Err, Tel: a.tel(inst),
			})
			return err
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm running a destructive script")
	return cmd
}

func newScriptsCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "scripts", Short: "List, build and catalog scripts"}
	cmd.AddCommand(newScriptsListCmd(a), newScriptsBuildCmd(a), newScriptsCatalogCmd(a))
	return cmd
}

func newScriptsListCmd(a *app) *cobra.Command {
	var domain, side string
	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "List scripts (id, side effect, runtime, summary)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			format, err := a.formatOr("line", "line", fmtJSON, fmtJSONL)
			if err != nil {
				return err
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			all, err := scripts.Load(inst)
			if err != nil {
				return err
			}
			var rows []scriptRow
			for _, s := range all {
				if (domain == "" || s.Domain == domain) && (side == "" || s.SideEffect == side) {
					rows = append(rows, scriptRow{ID: s.ID, Domain: s.Domain, SideEffect: s.SideEffect, Runtime: s.Runtime, Status: s.Status, Summary: s.Summary, Path: s.Dir + "/"})
				}
			}
			return a.printScripts(rows, format)
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "only this domain")
	cmd.Flags().StringVar(&side, "side", "", "only this side-effect class")
	return cmd
}

type scriptRow struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	SideEffect string `json:"side_effect"`
	Runtime    string `json:"runtime"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	Path       string `json:"path"`
}

func (a *app) printScripts(rows []scriptRow, format string) error {
	switch format {
	case fmtJSON:
		return render.JSON(a.io.Out, map[string]any{"v": 1, "scripts": nonNil(rows)})
	case fmtJSONL:
		return render.JSONL(a.io.Out, rows)
	}
	idW := 8
	for _, r := range rows {
		idW = max(idW, render.Runes(r.ID))
	}
	for _, r := range rows {
		prefix := fmt.Sprintf("%s  %s  %s  ", render.Pad(r.ID, min(idW, 40)), render.Pad(r.SideEffect, 12), render.Pad(r.Runtime, 6))
		text := r.Summary
		if r.Status == "deprecated" || r.Status == "draft" {
			text = "(" + r.Status + ") " + text
		}
		if err := a.printf("%s\n", strings.TrimRight(prefix+render.Truncate(render.OneLine(text), a.width()-render.Runes(prefix)), " ")); err != nil {
			return err
		}
	}
	return nil
}

func newScriptsBuildCmd(a *app) *cobra.Command {
	var all, force bool
	cmd := &cobra.Command{
		Use:   "build [<id>…|--all]",
		Short: "Prebuild Go scripts into the cache (.strata/bin)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if all == (len(args) > 0) {
				return fail.New(fail.Usage, "pass script ids or --all", "nothing to build")
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			list, err := scripts.Load(inst)
			if err != nil {
				return err
			}
			targets, err := buildTargets(list, args, all)
			if err != nil {
				return err
			}
			for i := range targets {
				res, err := scripts.Build(cmd.Context(), inst, &targets[i], force)
				if err != nil {
					return err
				}
				state := "cached"
				if res.Built {
					state = "built"
				}
				if err := a.printf("%s\t%s\n", res.ID, state); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "build every Go script")
	cmd.Flags().BoolVar(&force, "force", false, "rebuild even when the cache is current")
	return cmd
}

// buildTargets selects Go scripts by id (or all of them).
func buildTargets(list []scripts.Script, ids []string, all bool) ([]scripts.Script, error) {
	var out []scripts.Script
	if all {
		for _, s := range list {
			if s.Runtime == "go" {
				out = append(out, s)
			}
		}
		return out, nil
	}
	for _, id := range ids {
		found := false
		for _, s := range list {
			if s.ID == id {
				out, found = append(out, s), true
				break
			}
		}
		if !found {
			return nil, fail.New(fail.Usage, "see `strata scripts list`", "no script with id %q", id)
		}
	}
	return out, nil
}

func newScriptsCatalogCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "catalog",
		Short: "Regenerate scripts/CATALOG.md (per-domain catalogs above [scripts] catalog_split_at)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			changed, err := scripts.WriteCatalog(inst)
			for _, p := range changed {
				if perr := a.printf("%s\n", p); perr != nil {
					return perr
				}
			}
			return err
		},
	}
}
