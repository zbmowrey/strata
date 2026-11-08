package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// gh drives the GitHub CLI.
type gh struct {
	run Runner
	dir string
}

func (*gh) Name() string { return GitHub }

var rePRNumber = regexp.MustCompile(`/pulls?/(\d+)`)

func (g *gh) CreateDraft(ctx context.Context, branch, base, title, body string) (PR, error) {
	out, err := g.run(ctx, g.dir, "gh", "pr", "create", "--draft", "--head", branch, "--base", base, "--title", title, "--body", body)
	if err != nil {
		return PR{}, fmt.Errorf("create draft PR: %w", err)
	}
	return prFromOutput(string(out), branch)
}

// prFromOutput finds the PR URL (the last pull-request URL) in CLI output.
func prFromOutput(out, branch string) (PR, error) {
	var u string
	for f := range strings.FieldsSeq(out) {
		if (strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://")) && rePRNumber.MatchString(f) {
			u = f
		}
	}
	if u == "" {
		return PR{}, fmt.Errorf("no pull request URL in output %q", strings.TrimSpace(out))
	}
	n, err := strconv.Atoi(rePRNumber.FindStringSubmatch(u)[1])
	if err != nil {
		return PR{}, fmt.Errorf("pull request number in %q: %w", u, err)
	}
	return PR{Number: n, URL: u, Branch: branch}, nil
}

func (g *gh) MarkReady(ctx context.Context, ref string) error {
	if _, err := g.run(ctx, g.dir, "gh", "pr", "ready", ref); err != nil {
		return fmt.Errorf("mark PR %s ready: %w", ref, err)
	}
	return nil
}

func (g *gh) Comment(ctx context.Context, ref, body string) error {
	if _, err := g.run(ctx, g.dir, "gh", "pr", "comment", ref, "--body", body); err != nil {
		return fmt.Errorf("comment on PR %s: %w", ref, err)
	}
	return nil
}

func (g *gh) FindOpen(ctx context.Context, prefix string) ([]PR, error) {
	out, err := g.run(ctx, g.dir, "gh", "pr", "list", "--state", "open", "--limit", "200", "--json", "number,url,headRefName,files")
	if err != nil {
		return nil, fmt.Errorf("list open PRs: %w", err)
	}
	var raw []struct {
		URL   string `json:"url"`
		Head  string `json:"headRefName"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse gh pr list: %w", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, r := range raw {
		if !strings.HasPrefix(r.Head, prefix) {
			continue
		}
		p := PR{Number: r.Number, URL: r.URL, Branch: r.Head}
		for _, f := range r.Files {
			p.Files = append(p.Files, f.Path)
		}
		prs = append(prs, p)
	}
	return prs, nil
}
