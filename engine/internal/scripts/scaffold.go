package scripts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/scaffold"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

// Runtimes with scaffolding templates.
var scaffoldRuntimes = []string{RuntimeGo, RuntimePython}

// tmplData is the data passed to every script template.
type tmplData struct {
	ID      string
	Domain  string
	Module  string
	Project string
}

// NewOptions select what `strata new script` creates.
type NewOptions struct {
	Domain  string
	Name    string
	Runtime string
}

// Scaffold creates scripts/<domain>/<name>/ with a manifest whose TODO
// placeholders fail lint until filled, an entry point using the lib
// conventions, and a deliberately failing test. The runtime's module file
// and lib starters are created when missing (never overwritten). It returns
// the instance-relative paths created.
func Scaffold(ctx context.Context, inst *config.Instance, opt NewOptions) ([]string, error) {
	if opt.Runtime == "" {
		opt.Runtime = inst.Config.Scripts.DefaultRuntime
	}
	if !slices.Contains(scaffoldRuntimes, opt.Runtime) {
		return nil, fail.New(fail.Usage, "scaffolding supports --runtime go|python; other runtimes are written by hand", "no template for runtime %q", opt.Runtime)
	}
	for _, part := range []string{opt.Domain, opt.Name} {
		if !schema.IDPattern.MatchString(part) {
			return nil, fail.New(fail.Usage, "use strata new script <domain>/<name> in lowercase kebab-case (verb-first name, e.g. find-leaked-fixtures)", "invalid script path %q", opt.Domain+"/"+opt.Name)
		}
	}
	if len(opt.Name) < 3 || len(opt.Name) > 64 {
		return nil, fail.New(fail.Usage, "script ids are 3–64 chars", "invalid script name %q", opt.Name)
	}
	if _, err := Find(inst, opt.Name); err == nil {
		return nil, fail.New(fail.Usage, "script ids are globally unique; extend the existing script or choose another name", "script %q already exists", opt.Name)
	}
	sr := filepath.ToSlash(inst.Config.Paths.Scripts)
	dir := path.Join(sr, opt.Domain, opt.Name)
	if _, err := os.Stat(filepath.Join(inst.Root, filepath.FromSlash(dir))); err == nil {
		return nil, fail.New(fail.Usage, "pick another name", "%s already exists", dir)
	}
	created, err := WriteLib(ctx, inst, []string{opt.Runtime})
	if err != nil {
		return nil, err
	}
	data, err := templateData(ctx, inst, opt)
	if err != nil {
		return nil, err
	}
	files, err := renderDir(path.Join(scaffold.ScriptTemplateRoot, opt.Runtime), dir, data)
	if err != nil {
		return nil, err
	}
	written, err := writeNew(inst.Root, files)
	return append(created, written...), err
}

// WriteLib creates, for each runtime, the scripts module file and the shared
// lib starters (docs/spec/04-scriptorium.md §6) without overwriting
// anything. It backs `strata init --scripts` and is idempotent.
func WriteLib(ctx context.Context, inst *config.Instance, runtimes []string) ([]string, error) {
	data, err := templateData(ctx, inst, NewOptions{})
	if err != nil {
		return nil, err
	}
	sr := filepath.ToSlash(inst.Config.Paths.Scripts)
	files := map[string][]byte{}
	for _, rt := range runtimes {
		var tmplDir, module string
		switch rt {
		case RuntimeGo:
			tmplDir, module = "lib/go", "go.mod.tmpl"
		case RuntimePython:
			tmplDir, module = "lib/py", "pyproject.toml.tmpl"
		default:
			continue
		}
		lib, err := renderDir(path.Join(scaffold.ScriptTemplateRoot, tmplDir), path.Join(sr, tmplDir), data)
		if err != nil {
			return nil, err
		}
		for k, v := range lib {
			files[k] = v
		}
		mod, err := renderFile(path.Join(scaffold.ScriptTemplateRoot, module), data)
		if err != nil {
			return nil, err
		}
		files[path.Join(sr, strings.TrimSuffix(module, ".tmpl"))] = mod
	}
	return writeNew(inst.Root, files)
}

// templateData resolves the Go module path (from an existing scripts/go.mod,
// else the origin remote, else a local placeholder) and project name.
func templateData(ctx context.Context, inst *config.Instance, opt NewOptions) (tmplData, error) {
	d := tmplData{ID: opt.Name, Domain: opt.Domain, Project: filepath.Base(inst.Root)}
	modFile := filepath.Join(scriptsDir(inst), "go.mod")
	if b, err := os.ReadFile(modFile); err == nil { //nolint:gosec // G304: the instance's scripts module
		for _, line := range strings.Split(string(b), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
				d.Module = strings.TrimSpace(rest)
				return d, nil
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return d, fmt.Errorf("read %s: %w", modFile, err)
	}
	d.Module = ModulePath(originURL(ctx, inst.Root), d.Project)
	return d, nil
}

func originURL(ctx context.Context, root string) string {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var remoteRE = regexp.MustCompile(`^(?:[a-z+]+://)?(?:[^@/]+@)?([^/:]+)[:/](.+?)(?:\.git)?/?$`)

// ModulePath derives the scripts Go module path from a git remote URL
// (git@github.com:o/r.git → github.com/o/r/scripts), falling back to
// strata.local/<project>/scripts.
func ModulePath(remote, project string) string {
	if m := remoteRE.FindStringSubmatch(remote); len(m) == 3 && !strings.Contains(m[1], " ") {
		return strings.ToLower(m[1]) + "/" + m[2] + "/scripts"
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return '-'
	}, project)
	return "strata.local/" + name + "/scripts"
}

// renderDir renders every template under src into dst (relative paths).
func renderDir(src, dst string, data tmplData) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(scaffold.ScriptTemplates, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := renderFile(p, data)
		if err != nil {
			return err
		}
		out[path.Join(dst, strings.TrimSuffix(strings.TrimPrefix(p, src+"/"), ".tmpl"))] = b
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("render templates: %w", err)
	}
	return out, nil
}

func renderFile(p string, data tmplData) ([]byte, error) {
	raw, err := scaffold.ScriptTemplates.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", p, err)
	}
	t, err := template.New(path.Base(p)).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", p, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, fmt.Errorf("template %s: %w", p, err)
	}
	return b.Bytes(), nil
}

// writeNew writes files that do not exist yet, in sorted order, returning
// the paths written.
func writeNew(root string, files map[string][]byte) ([]string, error) {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var written []string
	for _, rel := range keys {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := fsutil.WriteAtomic(p, files[rel], 0o644); err != nil {
			return written, err
		}
		written = append(written, rel)
	}
	return written, nil
}
