package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// writer holds prepared statements for one refresh transaction.
type writer struct {
	tx    *sql.Tx
	stmts map[string]*sql.Stmt
}

var writerSQL = map[string]string{
	"docsByPath": "SELECT rowid, id FROM docs WHERE src = ?",
	"delDoc":     "DELETE FROM docs WHERE rowid = ?",
	"delFTS":     "DELETE FROM docs_fts WHERE rowid = ?",
	"delTri":     "DELETE FROM names_tri WHERE rowid = ?",
	"delTags":    "DELETE FROM tags WHERE doc = ?",
	"delAliases": "DELETE FROM aliases WHERE doc = ?",
	"delLinks":   "DELETE FROM links WHERE src = ?",
	"delSects":   "DELETE FROM sections WHERE doc = ?",
	"delApplies": "DELETE FROM applies_to WHERE doc = ?",
	"delFile":    "DELETE FROM files WHERE path = ?",
	"upFile":     "INSERT INTO files(path, size, mtime_ns, hash, kind) VALUES(?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET size=excluded.size, mtime_ns=excluded.mtime_ns, hash=excluded.hash, kind=excluded.kind",
	"touchFile":  "UPDATE files SET size = ?, mtime_ns = ? WHERE path = ?",
	"insDoc": `INSERT INTO docs(id, kind, path, src, anchor, domain, topic, type, status, trust, title, summary, created, updated,
		verified_at, review_after_days, side_effect, runtime, bytes, lines, tokens_est, private, meta_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
	"insFTS":     "INSERT INTO docs_fts(rowid, title, aliases, summary, tags, headings, body) VALUES(?,?,?,?,?,?,?)",
	"insTri":     "INSERT INTO names_tri(rowid, name) VALUES(?,?)",
	"insTag":     "INSERT INTO tags(doc, tag) VALUES(?,?)",
	"insAlias":   "INSERT OR IGNORE INTO aliases(alias, doc) VALUES(?,?)",
	"insLink":    "INSERT INTO links(src, dst_id, anchor, kind) VALUES(?,?,?,?)",
	"insSect":    "INSERT INTO sections(doc, ord, heading, slug, start_byte, end_byte) VALUES(?,?,?,?,?,?)",
	"insApplies": "INSERT INTO applies_to(doc, glob) VALUES(?,?)",
	"setMeta":    "INSERT INTO meta(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
}

func newWriter(ctx context.Context, tx *sql.Tx) (*writer, error) {
	w := &writer{tx: tx, stmts: make(map[string]*sql.Stmt, len(writerSQL))}
	for name, q := range writerSQL {
		st, err := tx.PrepareContext(ctx, q) //nolint:sqlclosecheck // closed by writer.close()
		if err != nil {
			w.close()
			return nil, fmt.Errorf("prepare %s: %w", name, err)
		}
		w.stmts[name] = st
	}
	return w, nil
}

func (w *writer) close() {
	for _, st := range w.stmts {
		_ = st.Close()
	}
}

func (w *writer) exec(ctx context.Context, name string, args ...any) error {
	if _, err := w.stmts[name].ExecContext(ctx, args...); err != nil {
		return fmt.Errorf("index %s: %w", name, err)
	}
	return nil
}

func (w *writer) setMeta(ctx context.Context, k, v string) error {
	return w.exec(ctx, "setMeta", k, v)
}

// deleteDocs removes every doc (and its satellite rows) indexed from path.
func (w *writer) deleteDocs(ctx context.Context, path string) error {
	ids, names, err := w.docsAt(ctx, path)
	if err != nil {
		return err
	}
	for _, id := range ids {
		for _, name := range []string{"delFTS", "delTri", "delTags", "delAliases", "delLinks", "delSects", "delApplies", "delDoc"} {
			if err := w.exec(ctx, name, id); err != nil {
				return err
			}
		}
	}
	return w.unshadow(ctx, names)
}

// docsAt lists the rowids and ids of docs indexed from a source file.
func (w *writer) docsAt(ctx context.Context, path string) (ids []int64, names []string, err error) {
	rows, err := w.stmts["docsByPath"].QueryContext(ctx, path)
	if err != nil {
		return nil, nil, fmt.Errorf("index lookup: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, nil, fmt.Errorf("index lookup: %w", err)
		}
		ids = append(ids, id)
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("index lookup: %w", err)
	}
	return ids, names, nil
}

// unshadow gives a freed bare id back to the best-ranked prefixed holder.
func (w *writer) unshadow(ctx context.Context, freed []string) error {
	for _, id := range freed {
		if strings.Contains(id, ":") {
			continue
		}
		var rowid int64
		// Explicit candidates keep this an index lookup on docs.id.
		err := w.tx.QueryRowContext(ctx, `SELECT rowid FROM docs WHERE id IN (?, ?, ?, ?, ?, ?, ?) ORDER BY CASE kind
			WHEN 'shard' THEN 0 WHEN 'hub' THEN 1 WHEN 'question' THEN 2 WHEN 'script' THEN 3 WHEN 'playbook' THEN 4 WHEN 'inbox' THEN 5 ELSE 6 END LIMIT 1`,
			"shard:"+id, "hub:"+id, "question:"+id, "script:"+id, "playbook:"+id, "inbox:"+id, "project:"+id).Scan(&rowid)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("index unshadow %s: %w", id, err)
		}
		if _, err := w.tx.ExecContext(ctx, "UPDATE docs SET id = ? WHERE rowid = ?", id, rowid); err != nil {
			return fmt.Errorf("index unshadow %s: %w", id, err)
		}
	}
	return nil
}

func (w *writer) deletePath(ctx context.Context, path string) error {
	if err := w.deleteDocs(ctx, path); err != nil {
		return err
	}
	return w.exec(ctx, "delFile", path)
}

func (w *writer) touch(ctx context.Context, c change) error {
	return w.exec(ctx, "touchFile", c.file.Size, c.file.ModNS, c.file.Rel)
}

// replace swaps a file's docs for freshly extracted records.
func (w *writer) replace(ctx context.Context, c change) error {
	if err := w.deleteDocs(ctx, c.file.Rel); err != nil {
		return err
	}
	for i := range c.recs {
		c.recs[i].Src = c.file.Rel
		if err := w.insert(ctx, &c.recs[i]); err != nil {
			return err
		}
	}
	return w.exec(ctx, "upFile", c.file.Rel, c.file.Size, c.file.ModNS, c.hash, string(c.file.Kind))
}

// kindRank orders kinds for id ownership: on a collision the lower rank
// keeps the bare id and the other is addressed as `<kind>:<id>`.
var kindRank = map[string]int{"shard": 0, "hub": 1, "question": 2, "script": 3, "playbook": 4, "inbox": 5, "project": 6}

// insert adds one record, resolving cross-kind id collisions by kind rank so
// the result does not depend on insertion order.
func (w *writer) insert(ctx context.Context, r *Record) error {
	res, err := w.insDoc(ctx, r)
	if isUnique(err) {
		if err := w.resolveCollision(ctx, r); err != nil {
			return err
		}
		res, err = w.insDoc(ctx, r)
	}
	if err != nil {
		return err
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("index rowid: %w", err)
	}
	return w.satellites(ctx, rowid, r)
}

// resolveCollision makes room for r: the better-ranked kind keeps the bare id.
func (w *writer) resolveCollision(ctx context.Context, r *Record) error {
	var otherKind string
	if err := w.tx.QueryRowContext(ctx, "SELECT kind FROM docs WHERE id = ?", r.ID).Scan(&otherKind); err != nil {
		return fmt.Errorf("index collision %s: %w", r.ID, err)
	}
	if kindRank[r.Kind] >= kindRank[otherKind] {
		r.ID = r.Kind + ":" + r.ID
		return nil
	}
	if _, err := w.tx.ExecContext(ctx, "UPDATE docs SET id = ? WHERE id = ?", otherKind+":"+r.ID, r.ID); err != nil {
		return fmt.Errorf("index collision %s: %w", r.ID, err)
	}
	return nil
}

func (w *writer) insDoc(ctx context.Context, r *Record) (sql.Result, error) {
	res, err := w.stmts["insDoc"].ExecContext(ctx, r.ID, r.Kind, r.Path, r.Src, r.Anchor, r.Domain, r.Topic, r.Type, r.Status, r.Trust,
		r.Title, r.Summary, r.Created, r.Updated, r.VerifiedAt, r.ReviewAfterDays, r.SideEffect, r.Runtime,
		r.Bytes, r.Lines, r.TokensEst(), boolInt(r.Private), r.MetaJSON())
	if err != nil {
		return nil, fmt.Errorf("index doc %s: %w", r.ID, err)
	}
	return res, nil
}

func (w *writer) satellites(ctx context.Context, rowid int64, r *Record) error {
	heads := make([]string, 0, len(r.Headings))
	for _, h := range r.Headings {
		heads = append(heads, h.Text)
	}
	// The id's words are indexed with the aliases so `deploy` finds deploy-flow.
	names := append([]string{idWords(r.ID)}, r.Aliases...)
	if err := w.exec(ctx, "insFTS", rowid, r.Title, strings.Join(names, " "), r.Summary, strings.Join(r.Tags, " "), strings.Join(heads, "\n"), r.Body); err != nil {
		return err
	}
	if err := w.exec(ctx, "insTri", rowid, strings.Join(append([]string{r.ID, r.Title}, r.Aliases...), " ")); err != nil {
		return err
	}
	for _, t := range r.Tags {
		if err := w.exec(ctx, "insTag", rowid, t); err != nil {
			return err
		}
	}
	for _, a := range r.Aliases {
		if err := w.exec(ctx, "insAlias", strings.ToLower(a), rowid); err != nil {
			return err
		}
	}
	for _, l := range r.Links {
		if err := w.exec(ctx, "insLink", rowid, l.Dst, l.Anchor, l.Kind); err != nil {
			return err
		}
	}
	for i, h := range r.Headings {
		if err := w.exec(ctx, "insSect", rowid, i, h.Text, h.Slug, h.Start, h.End); err != nil {
			return err
		}
	}
	for _, g := range r.AppliesTo {
		if err := w.exec(ctx, "insApplies", rowid, g); err != nil {
			return err
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// idWords turns an id like `ci-flake:log#2026-10-21` into searchable words.
func idWords(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ':' || r == '#' || r == '/' || r == '_' {
			return ' '
		}
		return r
	}, id)
}
