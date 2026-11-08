// Package forge abstracts the pull-request operations the gardener needs
// (docs/spec/07-gardener.md §8). Implementations shell out to the forge's
// own CLI (`gh`, `tea`); Strata never handles tokens.
package forge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

// PR is one pull request.
type PR struct {
	URL    string   `json:"url"`
	Branch string   `json:"branch"`
	Files  []string `json:"files,omitempty"`
	Number int      `json:"number"`
}

// Forge performs pull-request operations on the current repository.
type Forge interface {
	// Name is "github" or "forgejo".
	Name() string
	// CreateDraft opens a draft PR from branch into base.
	CreateDraft(ctx context.Context, branch, base, title, body string) (PR, error)
	// MarkReady marks a draft PR ready for review (ref: number or URL).
	MarkReady(ctx context.Context, ref string) error
	// Comment adds a comment to a PR.
	Comment(ctx context.Context, ref, body string) error
	// FindOpen lists open PRs whose head branch starts with prefix.
	FindOpen(ctx context.Context, prefix string) ([]PR, error)
}

// Kinds accepted by `[garden] forge`.
const (
	Auto    = "auto"
	GitHub  = "github"
	Forgejo = "forgejo"
)

// ErrNoCLI reports that the forge's CLI is not installed: the caller pushes
// the branch and prints CompareURL instead.
var ErrNoCLI = errors.New("forge CLI not found")

// NoCLIError names the missing CLI; it matches ErrNoCLI with errors.Is.
type NoCLIError struct {
	Forge string
	CLI   string
}

func (e *NoCLIError) Error() string {
	return fmt.Sprintf("%s CLI %q not found on PATH (install it, or open the PR from the compare URL)", e.Forge, e.CLI)
}

// Is makes errors.Is(err, ErrNoCLI) true.
func (e *NoCLIError) Is(target error) bool { return target == ErrNoCLI }

// Runner executes a command in dir and returns its stdout.
type Runner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

// ExecRunner runs real processes.
func ExecRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: fixed forge CLIs with engine-built arguments
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, firstArg(args), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// Open selects the forge for the repository at dir. kind is the
// `[garden] forge` value; "auto" inspects the origin remote's host.
func Open(ctx context.Context, dir, kind string) (Forge, error) {
	return open(ctx, dir, kind, ExecRunner, exec.LookPath)
}

func open(ctx context.Context, dir, kind string, run Runner, look func(string) (string, error)) (Forge, error) {
	if kind == "" || kind == Auto {
		remote, err := run(ctx, dir, "git", "remote", "get-url", "origin")
		if err != nil {
			return nil, fmt.Errorf("forge auto-detect: %w", err)
		}
		kind = Detect(strings.TrimSpace(string(remote)))
	}
	switch kind {
	case GitHub:
		if _, err := look("gh"); err != nil {
			return nil, &NoCLIError{Forge: GitHub, CLI: "gh"}
		}
		return &gh{dir: dir, run: run}, nil
	case Forgejo:
		if _, err := look("tea"); err != nil {
			return nil, &NoCLIError{Forge: Forgejo, CLI: "tea"}
		}
		return &tea{dir: dir, run: run}, nil
	}
	return nil, fmt.Errorf("unknown forge %q (want auto, github or forgejo)", kind)
}

// Detect maps a remote URL to a forge kind: github.com → github, anything
// else → forgejo.
func Detect(remote string) string {
	if host, _, ok := parseRemote(remote); ok && strings.EqualFold(host, "github.com") {
		return GitHub
	}
	return Forgejo
}

var scpLike = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// parseRemote extracts host and owner/repo path from https, ssh:// and
// scp-like (git@host:owner/repo.git) remotes.
func parseRemote(remote string) (host, path string, ok bool) {
	remote = strings.TrimSpace(remote)
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil || u.Host == "" {
			return "", "", false
		}
		return u.Hostname(), cleanPath(u.Path), true
	}
	if m := scpLike.FindStringSubmatch(remote); m != nil {
		return m[1], cleanPath(m[2]), true
	}
	return "", "", false
}

func cleanPath(p string) string {
	return strings.TrimSuffix(strings.Trim(p, "/"), ".git")
}

// CompareURL is the web URL for opening a PR from branch into base by hand
// (the `/compare/<base>...<branch>` form works on GitHub, Forgejo and Gitea).
func CompareURL(remote, base, branch string) (string, error) {
	host, path, ok := parseRemote(remote)
	if !ok || path == "" {
		return "", fmt.Errorf("cannot derive a web URL from remote %q", remote)
	}
	// Git ref names cannot contain spaces or `..`, so they are URL-safe as is
	// (slashes in branch names must stay unescaped for the compare view).
	return fmt.Sprintf("https://%s/%s/compare/%s...%s", host, path, base, branch), nil
}
