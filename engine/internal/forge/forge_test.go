package forge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCLI writes a fake forge CLI that logs argv (one arg per line, then a
// blank line) and prints canned output per subcommand.
func fakeCLI(t *testing.T, name string) (bin, log string) {
	t.Helper()
	bin = t.TempDir()
	log = filepath.Join(bin, "argv.log")
	script := `#!/bin/sh
for a in "$@"; do printf '%s\n' "$a" >> "` + log + `"; done
printf '\n' >> "` + log + `"
case "$1 $2" in
"pr create") printf '%s\n' "https://github.com/o/r/pull/42" ;;
"pr list") printf '%s\n' '[{"number":7,"url":"https://github.com/o/r/pull/7","headRefName":"garden/20261108-0600","files":[{"path":"knowledge/a.md"}]},{"number":8,"url":"u8","headRefName":"feature/x","files":[]}]' ;;
"pulls create") printf '%s\n' "#13 WIP: garden" "https://code.example.org/o/r/pulls/13" ;;
"pulls list") printf '%s\n' '[{"index":"13","url":"https://code.example.org/o/r/pulls/13","head":"garden/a"},{"index":"14","url":"u","head":"main"}]' ;;
"remote get-url") printf '%s\n' "$FAKE_REMOTE" ;;
"fail now") echo boom >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil { //nolint:gosec // G306: test fake must be executable
		t.Fatal(err)
	}
	return bin, log
}

func argv(t *testing.T, log string) []string {
	t.Helper()
	raw, err := os.ReadFile(log) //nolint:gosec // G304: the fake CLI argv log in t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n\n"), "\n\n")
}

func TestGitHub(t *testing.T) { //nolint:paralleltest // t.Setenv(PATH) forbids t.Parallel
	bin, log := fakeCLI(t, "gh")
	t.Setenv("PATH", bin)
	ctx := context.Background()
	f, err := Open(ctx, t.TempDir(), GitHub)
	if err != nil {
		t.Fatal(err)
	}
	pr, err := f.CreateDraft(ctx, "garden/x", "main", "Garden", "body text")
	if err != nil || pr.Number != 42 || pr.Branch != "garden/x" || f.Name() != GitHub {
		t.Fatalf("create: %+v %v", pr, err)
	}
	if err := f.MarkReady(ctx, "42"); err != nil {
		t.Fatal(err)
	}
	if err := f.Comment(ctx, "42", "hi"); err != nil {
		t.Fatal(err)
	}
	prs, err := f.FindOpen(ctx, "garden/")
	if err != nil || len(prs) != 1 || prs[0].Number != 7 || len(prs[0].Files) != 1 {
		t.Fatalf("find: %+v %v", prs, err)
	}
	got := argv(t, log)
	want := []string{
		"pr\ncreate\n--draft\n--head\ngarden/x\n--base\nmain\n--title\nGarden\n--body\nbody text",
		"pr\nready\n42",
		"pr\ncomment\n42\n--body\nhi",
		"pr\nlist\n--state\nopen\n--limit\n200\n--json\nnumber,url,headRefName,files",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("argv:\n%q\nwant\n%q", got, want)
	}
}

func TestForgejo(t *testing.T) { //nolint:paralleltest // t.Setenv(PATH) forbids t.Parallel
	bin, log := fakeCLI(t, "tea")
	t.Setenv("PATH", bin)
	ctx := context.Background()
	f, err := Open(ctx, t.TempDir(), Forgejo)
	if err != nil {
		t.Fatal(err)
	}
	pr, err := f.CreateDraft(ctx, "garden/a", "main", "Garden", "b")
	if err != nil || pr.Number != 13 || f.Name() != Forgejo {
		t.Fatalf("create: %+v %v", pr, err)
	}
	if err := f.MarkReady(ctx, pr.URL); err != nil {
		t.Fatal(err)
	}
	if err := f.Comment(ctx, "13", "c"); err != nil {
		t.Fatal(err)
	}
	prs, err := f.FindOpen(ctx, "garden/")
	if err != nil || len(prs) != 1 || prs[0].Number != 13 {
		t.Fatalf("find: %+v %v", prs, err)
	}
	got := argv(t, log)
	if got[1] != "pulls\nedit\n--ready\n13" || got[2] != "comment\n13\nc" || !strings.HasPrefix(got[0], "pulls\ncreate\n--draft") {
		t.Fatalf("argv %q", got)
	}
}

func TestAutoDetectAndMissingCLI(t *testing.T) { //nolint:paralleltest // t.Setenv(PATH) forbids t.Parallel
	bin, _ := fakeCLI(t, "git")
	ghBin, _ := fakeCLI(t, "gh")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+ghBin)
	ctx := context.Background()
	t.Setenv("FAKE_REMOTE", "git@github.com:o/r.git")
	f, err := Open(ctx, t.TempDir(), Auto)
	if err != nil || f.Name() != GitHub {
		t.Fatalf("auto github: %v", err)
	}
	t.Setenv("FAKE_REMOTE", "https://code.example.org/o/r.git")
	_, err = Open(ctx, t.TempDir(), "")
	var nc *NoCLIError
	if !errors.Is(err, ErrNoCLI) || !errors.As(err, &nc) || nc.CLI != "tea" || !strings.Contains(err.Error(), "tea") {
		t.Fatalf("missing tea: %v", err)
	}
	if _, err := Open(ctx, t.TempDir(), "gitlab"); err == nil {
		t.Fatal("unknown forge accepted")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Open(ctx, t.TempDir(), GitHub); !errors.Is(err, ErrNoCLI) {
		t.Fatalf("missing gh: %v", err)
	}
	if _, err := Open(ctx, t.TempDir(), Auto); err == nil {
		t.Fatal("auto without git should fail")
	}
}

func TestCLIErrors(t *testing.T) { //nolint:paralleltest // t.Setenv(PATH) forbids t.Parallel
	bin := t.TempDir()
	for _, name := range []string{"gh", "tea"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho nope >&2\nexit 3\n"), 0o755); err != nil { //nolint:gosec // G306: test fake must be executable
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	ctx := context.Background()
	for _, kind := range []string{GitHub, Forgejo} {
		f, err := Open(ctx, t.TempDir(), kind)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.CreateDraft(ctx, "b", "main", "t", "x"); err == nil || !strings.Contains(err.Error(), "nope") {
			t.Errorf("%s create: %v", kind, err)
		}
		if f.MarkReady(ctx, "1") == nil || f.Comment(ctx, "1", "x") == nil {
			t.Errorf("%s: errors swallowed", kind)
		}
		if _, err := f.FindOpen(ctx, ""); err == nil {
			t.Errorf("%s find: no error", kind)
		}
	}
}

func TestParsing(t *testing.T) {
	t.Parallel()
	if _, err := prFromOutput("created, no url", "b"); err == nil {
		t.Fatal("no URL accepted")
	}
	g := &gh{run: func(context.Context, string, string, ...string) ([]byte, error) { return []byte("not json"), nil }}
	if _, err := g.FindOpen(context.Background(), ""); err == nil {
		t.Fatal("bad gh json accepted")
	}
	tt := &tea{run: func(context.Context, string, string, ...string) ([]byte, error) {
		return []byte(`[{"index":"x","head":"g"}]`), nil
	}}
	if _, err := tt.FindOpen(context.Background(), "g"); err == nil {
		t.Fatal("bad tea index accepted")
	}
	tt.run = func(context.Context, string, string, ...string) ([]byte, error) { return []byte("{"), nil }
	if _, err := tt.FindOpen(context.Background(), ""); err == nil {
		t.Fatal("bad tea json accepted")
	}
	if Detect("garbage") != Forgejo || Detect("https://GitHub.com/o/r") != GitHub {
		t.Fatal("Detect")
	}
	if firstArg(nil) != "" {
		t.Fatal("firstArg")
	}
}

func TestCompareURL(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://github.com/o/r.git":          "https://github.com/o/r/compare/main...garden/x",
		"git@github.com:o/r.git":              "https://github.com/o/r/compare/main...garden/x",
		"ssh://git@code.example.org:2222/o/r": "https://code.example.org/o/r/compare/main...garden/x",
		"https://code.example.org/o/r/":       "https://code.example.org/o/r/compare/main...garden/x",
	}
	for remote, want := range cases {
		got, err := CompareURL(remote, "main", "garden/x")
		if err != nil || got != want {
			t.Errorf("CompareURL(%q) = %q, %v; want %q", remote, got, err, want)
		}
	}
	for _, bad := range []string{"", "https://", "/local/path"} {
		if _, err := CompareURL(bad, "main", "b"); err == nil {
			t.Errorf("CompareURL(%q) accepted", bad)
		}
	}
}
