package gates

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestFormat(t *testing.T) {
	t.Parallel()
	if Format(&Report{Stage: StageStop}) != "" {
		t.Fatal("nothing blocks → empty")
	}
	rep := &Report{Stage: StageStop, Baselined: 3, Blocking: []finding.Finding{
		{Gate: "go-lint", Rule: "gocyclo", Severity: finding.Error, File: "engine/a.go", Line: 12, Col: 1, Message: "cyclomatic complexity 22", FixHint: "split the function", Docs: "go-complexity-limits"},
		{Gate: "go-lint", Rule: "errcheck", Severity: finding.Error, File: "engine/a.go", Line: 40, Col: 7, Message: "unchecked error", Docs: "https://golangci-lint.run"},
		{Gate: "go-test", Severity: finding.Error, File: "engine/b_test.go", Line: 9, Message: "TestX: nope\nmore"},
		{Gate: "go-vuln", Rule: "exit", Severity: finding.Error, Message: "vulnerable"},
	}}
	testutil.Golden(t, filepath.Join("testdata", "golden", "format.golden"), []byte(Format(rep)))
	if got := Summary(rep); got != "4 blocking · 4 errors · 0 warnings · 3 baselined" {
		t.Fatalf("summary %q", got)
	}

	var many []finding.Finding
	for i := range 60 {
		many = append(many, finding.Finding{Gate: "g", Rule: "r", Severity: finding.Error, File: fmt.Sprintf("f%02d.go", i/3), Line: i, Message: "m"})
	}
	out := Format(&Report{Stage: StageCommit, Blocking: many})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) > MaxReportLines {
		t.Fatalf("%d lines > %d", len(lines), MaxReportLines)
	}
	if !strings.Contains(out, "more (strata check --format json)") || !strings.HasPrefix(out, "strata gates ✗ 60 blocking findings (commit stage)") {
		t.Fatalf("overflow:\n%s", out)
	}
	one := Format(&Report{Stage: StageEdit, Blocking: many[:1]})
	if !strings.HasPrefix(one, "strata gates ✗ 1 blocking finding (edit stage)\n") {
		t.Fatalf("singular:\n%s", one)
	}
}

func TestExplain(t *testing.T) {
	t.Parallel()
	cfg := cfgOf(t, `
[[gate]]
id = "go-lint"
cmd = ["golangci-lint", "run", "{targets}"]
parser = "golangci-json"
[[gate]]
id = "suppressions"
builtin = "suppressions"
`)
	e, err := Explain(cfg, "go-lint/gocyclo")
	if err != nil || e.Rule != "gocyclo" || !strings.Contains(e.ToolDocs, "#gocyclo") || e.Command != "golangci-lint run {targets}" {
		t.Fatalf("%+v %v", e, err)
	}
	e, err = Explain(cfg, "suppressions")
	if err != nil || e.Command != "built-in: suppressions" || e.ToolDocs != "" {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := Explain(cfg, "nope/x"); err == nil {
		t.Fatal("unknown gate")
	}
	for _, p := range []string{ParserRuff, ParserSemgrep, ParserShellcheck, ParserClippy, ParserPyright, ParserTSC, ParserGitleaks} {
		if docsURL(p, "R1") == "" {
			t.Errorf("docsURL(%s)", p)
		}
	}
}
