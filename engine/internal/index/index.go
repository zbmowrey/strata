package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (D-003)

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// Paths of index files relative to the instance root.
const (
	DBFile   = ".strata/index.db"
	LockFile = ".strata/index.lock"
)

// Index is an open index over one instance.
type Index struct {
	db     *sql.DB
	inst   *config.Instance
	x      *extractor
	path   string
	Events []string // notable events (rebuilds, recoveries) for telemetry/verbose output
}

// Options control opening and refreshing.
type Options struct {
	NoRefresh   bool // skip the freshness check
	StrictFresh bool // wait for the writer lock instead of reading the current snapshot
}

// Stats describes one refresh.
type Stats struct {
	Elapsed time.Duration
	Files   int
	Added   int
	Changed int
	Removed int
	Rebuilt bool
	Skipped bool // another process held the lock; read the current snapshot
}

// Open opens (creating or rebuilding as needed) the instance's index and,
// unless opts.NoRefresh, brings it up to date with the files on disk.
func Open(ctx context.Context, inst *config.Instance, opts Options) (*Index, Stats, error) {
	ix := &Index{inst: inst, path: inst.Path(DBFile), x: &extractor{layout: corpus.LayoutOf(inst.Config.Paths), cfg: inst.Config}}
	if err := ix.openDB(ctx); err != nil {
		if err := ix.lockedReset(ctx, "recover: "+err.Error()); err != nil {
			return nil, Stats{}, err
		}
	}
	if opts.NoRefresh {
		return ix, Stats{Skipped: true}, nil
	}
	st, err := ix.Refresh(ctx, opts.StrictFresh)
	if err != nil {
		_ = ix.Close()
		return nil, st, err
	}
	return ix, st, nil
}

// DB exposes the read connection for query packages.
func (ix *Index) DB() *sql.DB { return ix.db }

// Instance returns the indexed instance.
func (ix *Index) Instance() *config.Instance { return ix.inst }

// Close closes the database.
func (ix *Index) Close() error {
	if ix.db == nil {
		return nil
	}
	err := ix.db.Close()
	ix.db = nil
	if err != nil {
		return fmt.Errorf("close index: %w", err)
	}
	return nil
}

func dsn(path string) string {
	return "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(OFF)&_txlock=immediate"
}

// openDB opens the database and verifies its schema version; on mismatch
// it recreates the schema (the index is disposable).
func (ix *Index) openDB(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(ix.path), 0o750); err != nil {
		return fmt.Errorf("index dir: %w", err)
	}
	db, err := sql.Open("sqlite", dsn(ix.path))
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	db.SetMaxOpenConns(4)
	ix.db = db
	var v int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("read index version: %w", err)
	}
	if v == SchemaVersion {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM meta").Scan(&n); err != nil {
			return fmt.Errorf("index unreadable: %w", err)
		}
		return nil
	}
	reason := "create"
	if v != 0 {
		reason = fmt.Sprintf("schema version %d != %d: rebuild", v, SchemaVersion)
	}
	return ix.lockedReset(ctx, reason)
}

// lockedReset recreates the schema under the writer lock, unless another
// process already did so while we waited.
func (ix *Index) lockedReset(ctx context.Context, reason string) error {
	lock, err := fsutil.LockWait(ix.inst.Path(LockFile), 30*time.Second)
	if err != nil {
		return lockErr(err)
	}
	defer func() { _ = lock.Unlock() }()
	if ix.db != nil {
		var v int
		if err := ix.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err == nil && v == SchemaVersion {
			var n int
			if err := ix.db.QueryRowContext(ctx, "SELECT count(*) FROM meta").Scan(&n); err == nil {
				return nil
			}
		}
	}
	ix.Events = append(ix.Events, reason)
	return ix.reset(ctx)
}

// reset deletes the database files and creates an empty schema.
func (ix *Index) reset(ctx context.Context) error {
	if ix.db != nil {
		_ = ix.db.Close()
		ix.db = nil
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(ix.path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove index: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dsn(ix.path))
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	db.SetMaxOpenConns(4)
	ix.db = db
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range schemaSQL {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("create index schema: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion)); err != nil {
		return fmt.Errorf("set index version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	return nil
}

// Rebuild drops and recreates the whole index.
func (ix *Index) Rebuild(ctx context.Context) (Stats, error) {
	lock, err := fsutil.LockWait(ix.inst.Path(LockFile), 30*time.Second)
	if err != nil {
		return Stats{}, lockErr(err)
	}
	defer func() { _ = lock.Unlock() }()
	if err := ix.reset(ctx); err != nil {
		return Stats{}, err
	}
	st, err := ix.refreshLocked(ctx)
	st.Rebuilt = true
	return st, err
}

func lockErr(err error) error {
	if errors.Is(err, fsutil.ErrLocked) {
		return fail.Wrap(fail.Locked, "another strata process is refreshing the index; retry", err)
	}
	return err
}
