package gates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

type findingT = finding.Finding

// sh runs a command in dir, failing the test on error.
func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: test helper running git
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// repo creates a git-initialized instance with the given files committed.
func repo(t *testing.T, files map[string]string) (*config.Instance, string) {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, ".strata/config.toml", "")
	testutil.WriteFile(t, dir, ".gitignore", ".strata/cache/\nignored.txt\n")
	for rel, content := range files {
		testutil.WriteFile(t, dir, rel, content)
	}
	sh(t, dir, "git", "init", "-q")
	sh(t, dir, "git", "add", "-A")
	sh(t, dir, "git", "commit", "-q", "-m", "init")
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst, dir
}

// tool writes an executable shell script and returns its absolute path.
func tool(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { //nolint:gosec // G306: test tool must be executable
		t.Fatal(err)
	}
	return p
}

// countingGrep returns a fake checker that reports lines containing word
// (file:line: text) and appends one line per invocation to a counter file.
func countingGrep(t *testing.T, word string) (bin, counter string) {
	t.Helper()
	counter = filepath.Join(t.TempDir(), "count")
	bin = tool(t, "check-"+strings.ToLower(word), "echo run >> '"+counter+"'\ngrep -Hn '"+word+"' \"$@\"\nexit 0")
	return bin, counter
}

func runs(t *testing.T, counter string) int {
	t.Helper()
	b, err := os.ReadFile(counter) //nolint:gosec // G304: test counter file
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "run")
}
