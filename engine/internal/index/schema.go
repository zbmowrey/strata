// Package index maintains the SQLite FTS5 index over an instance's corpus
// (docs/spec/03-search.md §3): schema, incremental refresh, locking, and
// corruption recovery. The index is disposable; markdown is the truth.
package index

// SchemaVersion is stored in PRAGMA user_version; a mismatch rebuilds.
const SchemaVersion = 1

// FTSColumns is the FTS column order; bm25 weights are passed in this order.
var FTSColumns = []string{"title", "aliases", "summary", "tags", "headings", "body"}

var schemaSQL = []string{
	`CREATE TABLE files (path TEXT PRIMARY KEY, size INT, mtime_ns INT, hash BLOB, kind TEXT) WITHOUT ROWID`,
	`CREATE TABLE docs (
		rowid INTEGER PRIMARY KEY,
		id TEXT UNIQUE NOT NULL,
		kind TEXT NOT NULL, path TEXT NOT NULL, src TEXT NOT NULL, anchor TEXT,
		domain TEXT, topic TEXT, type TEXT, status TEXT, trust TEXT,
		title TEXT NOT NULL, summary TEXT, created TEXT, updated TEXT, verified_at TEXT, review_after_days INT,
		side_effect TEXT, runtime TEXT,
		bytes INT, lines INT, tokens_est INT,
		private INT NOT NULL DEFAULT 0,
		meta_json TEXT)`,
	`CREATE INDEX docs_src ON docs(src)`,
	`CREATE INDEX docs_kind ON docs(kind)`,
	`CREATE VIRTUAL TABLE docs_fts USING fts5(title, aliases, summary, tags, headings, body,
		content='', contentless_delete=1, tokenize='porter unicode61 remove_diacritics 2')`,
	`CREATE VIRTUAL TABLE names_tri USING fts5(name, content='', contentless_delete=1, tokenize='trigram')`,
	`CREATE TABLE tags (doc INT, tag TEXT)`,
	`CREATE INDEX tags_tag ON tags(tag)`,
	`CREATE INDEX tags_doc ON tags(doc)`,
	`CREATE TABLE aliases (alias TEXT PRIMARY KEY, doc INT) WITHOUT ROWID`,
	`CREATE INDEX aliases_doc ON aliases(doc)`,
	`CREATE TABLE links (src INT, dst_id TEXT, anchor TEXT, kind TEXT)`,
	`CREATE INDEX links_dst ON links(dst_id)`,
	`CREATE INDEX links_src ON links(src)`,
	`CREATE TABLE sections (doc INT, ord INT, heading TEXT, slug TEXT, start_byte INT, end_byte INT)`,
	`CREATE INDEX sections_doc ON sections(doc)`,
	`CREATE TABLE applies_to (doc INT, glob TEXT)`,
	`CREATE INDEX applies_doc ON applies_to(doc)`,
	`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT) WITHOUT ROWID`,
}
