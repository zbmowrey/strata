package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
)

// run executes the CLI in-process and returns stdout, stderr and exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Main(args, Streams{In: strings.NewReader(""), Out: &out, Err: &errb})
	return out.String(), errb.String(), code
}

func TestVersion(t *testing.T) {
	t.Parallel()
	out, _, code := run(t, "version")
	if code != 0 || !strings.HasPrefix(out, "strata ") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	out, _, code = run(t, "version", "--format", "json")
	var v struct {
		Version string `json:"version"`
		V       int    `json:"v"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v.V != 1 || v.Version == "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"nope"}, {"version", "--bogus"}, {"version", "--format", "table"}, {"version", "extra"}} {
		_, errOut, code := run(t, args...)
		if code != fail.Usage {
			t.Errorf("%v: code=%d stderr=%q", args, code, errOut)
		}
		if !strings.HasPrefix(errOut, "strata: ") {
			t.Errorf("%v: stderr=%q", args, errOut)
		}
	}
}

func TestJSONErrorPayload(t *testing.T) {
	t.Parallel()
	out, _, code := run(t, "--format", "json", "version", "--nope")
	if code != fail.Usage {
		t.Fatalf("code=%d", code)
	}
	var p struct {
		Error struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil || p.Error.Code != fail.Usage {
		t.Fatalf("payload %q: %v", out, err)
	}
}

func TestPanicRecovered(t *testing.T) {
	t.Parallel()
	var errb bytes.Buffer
	code := Main([]string{"version"}, Streams{In: strings.NewReader(""), Out: panicWriter{}, Err: &errb})
	if code != fail.Runtime || !strings.Contains(errb.String(), "internal error") {
		t.Fatalf("code=%d err=%q", code, errb.String())
	}
}

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("boom") }

func TestOSStreams(t *testing.T) {
	t.Parallel()
	s := OSStreams()
	if s.In == nil || s.Out == nil || s.Err == nil {
		t.Fatal("nil stream")
	}
}
