package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
)

func TestLintCommand(t *testing.T) {
	t.Parallel()
	_, errOut, code := run(t, "--root", "../../testdata/instance-basic", "lint")
	if code != 0 || !strings.Contains(errOut, "54 files checked") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	out, _, code := run(t, "--root", "../../testdata/lint-invalid", "--format", "json", "lint", "--kind", "script")
	if code != fail.Blocking {
		t.Fatalf("code=%d", code)
	}
	var rep struct {
		Findings []map[string]any `json:"findings"`
		Checked  int              `json:"checked"`
		Errors   int              `json:"errors"`
	}
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&rep); err != nil || rep.Checked != 3 || rep.Errors == 0 {
		t.Fatalf("json %q: %v", out, err)
	}
	out, _, code = run(t, "--root", "../../testdata/lint-invalid", "--format", "jsonl", "lint", "--kind", "hub")
	if code != fail.Blocking || strings.Count(out, "\n") != 2 {
		t.Fatalf("jsonl code=%d out=%q", code, out)
	}
	out, _, code = run(t, "--root", "../../testdata/lint-invalid", "-q", "lint", "--kind", "inbox")
	if code != fail.Blocking || !strings.Contains(out, "schema/required") {
		t.Fatalf("line code=%d out=%q", code, out)
	}
}

func TestLintUsageErrors(t *testing.T) {
	t.Parallel()
	if _, _, code := run(t, "--root", "../../testdata/instance-basic", "lint", "--kind", "nope"); code != fail.Usage {
		t.Fatalf("bad kind code=%d", code)
	}
	if _, _, code := run(t, "--root", "../../testdata/instance-basic", "lint", "/"); code != fail.Usage {
		t.Fatalf("outside path code=%d", code)
	}
	if _, _, code := run(t, "--root", "../../testdata/instance-basic", "--format", "md", "lint"); code != fail.Usage {
		t.Fatalf("bad format code=%d", code)
	}
	if _, _, code := run(t, "--root", t.TempDir(), "lint"); code != fail.NotInstance {
		t.Fatalf("not instance code=%d", code)
	}
	if _, _, code := run(t, "--root", "../../testdata/instance-basic", "-v", "lint", "../../testdata/instance-basic/knowledge"); code != 0 {
		t.Fatalf("path arg code=%d", code)
	}
}
