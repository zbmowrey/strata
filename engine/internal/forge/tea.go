package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// tea drives the Gitea/Forgejo CLI. Drafts are WIP-prefixed titles, which
// Gitea and Forgejo treat as drafts (`tea pulls create --draft`).
type tea struct {
	run Runner
	dir string
}

func (*tea) Name() string { return Forgejo }

func (t *tea) CreateDraft(ctx context.Context, branch, base, title, body string) (PR, error) {
	out, err := t.run(ctx, t.dir, "tea", "pulls", "create", "--draft", "--head", branch, "--base", base, "--title", title, "--description", body)
	if err != nil {
		return PR{}, fmt.Errorf("create draft PR: %w", err)
	}
	return prFromOutput(string(out), branch)
}

// index accepts a PR number or URL and returns the number tea expects.
func index(ref string) string {
	if m := rePRNumber.FindStringSubmatch(ref); m != nil {
		return m[1]
	}
	return ref
}

func (t *tea) MarkReady(ctx context.Context, ref string) error {
	if _, err := t.run(ctx, t.dir, "tea", "pulls", "edit", "--ready", index(ref)); err != nil {
		return fmt.Errorf("mark PR %s ready: %w", ref, err)
	}
	return nil
}

func (t *tea) Comment(ctx context.Context, ref, body string) error {
	if _, err := t.run(ctx, t.dir, "tea", "comment", index(ref), body); err != nil {
		return fmt.Errorf("comment on PR %s: %w", ref, err)
	}
	return nil
}

func (t *tea) FindOpen(ctx context.Context, prefix string) ([]PR, error) {
	out, err := t.run(ctx, t.dir, "tea", "pulls", "list", "--state", "open", "--limit", "200", "--fields", "index,url,head", "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("list open PRs: %w", err)
	}
	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse tea pulls list: %w", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, r := range raw {
		head := fmt.Sprint(r["head"])
		if !strings.HasPrefix(head, prefix) {
			continue
		}
		n, err := strconv.Atoi(fmt.Sprint(r["index"]))
		if err != nil {
			return nil, fmt.Errorf("tea pulls list: index %v: %w", r["index"], err)
		}
		prs = append(prs, PR{Number: n, URL: fmt.Sprint(r["url"]), Branch: head})
	}
	return prs, nil
}
