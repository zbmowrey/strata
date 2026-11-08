package gates

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func readTool(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "tools", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsersGolden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		parser, file string
		exit         int
		gate         *Gate
	}{
		{ParserGolangci, "golangci.json", 1, nil},
		{ParserGoTest, "gotest.json", 1, nil},
		{ParserShellcheck, "shellcheck.json", 1, nil},
		{ParserRuff, "ruff.json", 1, nil},
		{ParserTSC, "tsc.txt", 2, nil},
		{ParserSemgrep, "semgrep.sarif", 1, nil},
		{ParserSARIF, "semgrep.sarif", 1, &Gate{Severity: "warning"}},
		{ParserGitleaks, "gitleaks.json", 1, nil},
		{ParserPyright, "pyright.json", 1, nil},
		{ParserESLint, "eslint.json", 1, nil},
		{ParserClippy, "clippy.json", 101, nil},
		{ParserLines, "typos.txt", 2, &Gate{Severity: "warning"}},
	}
	for _, tc := range cases {
		t.Run(tc.parser, func(t *testing.T) {
			t.Parallel()
			fs, err := ParseOutput(tc.parser, Output{Stdout: readTool(t, tc.file), Exit: tc.exit}, tc.gate)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) == 0 {
				t.Fatal("no findings parsed")
			}
			var b bytes.Buffer
			if err := render.JSONL(&b, fs); err != nil {
				t.Fatal(err)
			}
			testutil.Golden(t, filepath.Join("testdata", "golden", "parse-"+tc.parser+".golden"), b.Bytes())
		})
	}
}

func TestParserEdges(t *testing.T) {
	t.Parallel()
	if _, err := ParseOutput("nope", Output{}, nil); err == nil {
		t.Fatal("unknown parser")
	}
	for _, p := range []string{ParserGolangci, ParserRuff, ParserPyright, ParserESLint, ParserSARIF, ParserGitleaks, ParserShellcheck} {
		if _, err := ParseOutput(p, Output{Stdout: []byte("not json")}, nil); err == nil {
			t.Errorf("%s accepted garbage", p)
		}
		if _, err := ParseOutput(p, Output{}, nil); err == nil {
			t.Errorf("%s accepted empty output", p)
		}
	}
	fs, err := ParseOutput(ParserShellcheck, Output{Stdout: []byte(`{"comments":[{"file":"a.sh","line":1,"column":2,"level":"error","code":1000,"message":"m"}]}`)}, nil)
	if err != nil || len(fs) != 1 || fs[0].Rule != "SC1000" {
		t.Fatalf("json1 %v %v", fs, err)
	}
	fs, _ = ParseOutput(ParserExitcode, Output{Exit: 0, Stdout: []byte("fine")}, nil)
	if fs != nil {
		t.Fatal("exit 0 finding")
	}
	long := bytes.Repeat([]byte("line\n"), 30)
	fs, _ = ParseOutput(ParserExitcode, Output{Exit: 3, Stdout: long}, nil)
	if len(fs) != 1 || !bytes.Contains([]byte(fs[0].Message), []byte("10 more lines")) {
		t.Fatalf("exitcode %v", fs)
	}
	g := &Gate{LinesRegex: `^(?P<file>\S+\.go)$`, Desc: "not formatted"}
	fs, _ = ParseOutput(ParserLines, Output{Stdout: []byte("a.go\nb.go\n")}, g)
	if len(fs) != 2 || fs[0].Message != "not formatted" || fs[1].File != "b.go" {
		t.Fatalf("lines %v", fs)
	}
	if _, err := ParseOutput(ParserLines, Output{}, &Gate{LinesRegex: `(`}); err == nil {
		t.Fatal("bad regex")
	}
	if _, err := compileLines(`^(?P<x>.*)$`); err == nil {
		t.Fatal("regex without file group")
	}
	// go test: a build failure (package fails, no failing tests).
	buildFail := `{"Action":"output","Package":"ex/p","Output":"p/x.go:3:1: syntax error\n"}
{"Action":"fail","Package":"ex/p"}
`
	fs, _ = ParseOutput(ParserGoTest, Output{Stdout: []byte(buildFail)}, nil)
	if len(fs) != 1 || fs[0].Rule != "build" {
		t.Fatalf("build failure %v", fs)
	}
	for in, want := range map[string]string{"fatal": "error", "medium": "warning", "note": "info", "weird": "warning", "": "error"} {
		if got := severity(in, ""); got != want {
			t.Errorf("severity(%q) = %s", in, got)
		}
	}
	if docsURL(ParserESLint, "no-undef") == "" || docsURL(ParserESLint, "@ts/x") == "" || docsURL("lines", "x") != "" || docsURL(ParserGolangci, "") != "" {
		t.Fatal("docsURL")
	}
}

func FuzzParsers(f *testing.F) {
	for _, name := range []string{"golangci.json", "gotest.json", "shellcheck.json", "ruff.json", "tsc.txt", "semgrep.sarif", "gitleaks.json", "pyright.json", "eslint.json", "clippy.json", "typos.txt"} {
		b, err := os.ReadFile(filepath.Join("testdata", "tools", name))
		if err == nil {
			f.Add(b)
		}
	}
	f.Add([]byte(`{"Issues":[{"Pos":{}}]}`))
	f.Add([]byte(`[{"messages":[{}]}]`))
	names := make([]string, 0, len(parsers))
	for n := range parsers {
		names = append(names, n)
	}
	f.Fuzz(func(_ *testing.T, raw []byte) {
		for _, n := range names {
			_, _ = ParseOutput(n, Output{Stdout: raw, Stderr: raw, Exit: 1}, &Gate{})
		}
		_ = scanSuppressions("f.go", string(raw))
	})
}
