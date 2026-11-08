package index

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/zeebo/xxh3"

	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

type fileRow struct {
	hash  []byte
	size  int64
	mtime int64
}

// change is one file to (re)index.
type change struct {
	recs []Record
	hash []byte
	file corpus.File
	err  error
}

// Refresh brings the index up to date. Without changes it takes no lock and
// writes nothing. When another process holds the writer lock it returns
// Stats.Skipped (reading the current snapshot) unless strict is set.
func (ix *Index) Refresh(ctx context.Context, strict bool) (Stats, error) {
	start := time.Now()
	files, known, err := ix.scan(ctx)
	if err != nil {
		return Stats{}, err
	}
	plan := diff(files, known)
	if plan.empty() {
		return Stats{Files: len(files), Elapsed: time.Since(start)}, nil
	}
	lock, err := fsutil.TryLock(ix.inst.Path(LockFile))
	waited := false
	if errors.Is(err, fsutil.ErrLocked) {
		if !strict && ix.populated(ctx) {
			return Stats{Files: len(files), Skipped: true, Elapsed: time.Since(start)}, nil
		}
		// Strict mode, or no snapshot exists yet: wait for the writer
		// instead of answering from a stale or empty index.
		lock, err = fsutil.LockWait(ix.inst.Path(LockFile), 30*time.Second)
		waited = true
	}
	if err != nil {
		return Stats{}, lockErr(err)
	}
	defer func() { _ = lock.Unlock() }()
	if waited {
		st, err := ix.refreshLocked(ctx)
		st.Elapsed = time.Since(start)
		return st, err
	}
	// We took the lock without waiting: the walk above is current; only the
	// files table needs re-reading in case another writer finished meanwhile.
	if known, err = ix.loadFiles(ctx); err != nil {
		return Stats{}, err
	}
	st, err := ix.apply(ctx, files, known)
	st.Elapsed = time.Since(start)
	return st, err
}

// populated reports whether a refresh has ever completed.
func (ix *Index) populated(ctx context.Context) bool {
	var v string
	return ix.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'last_refresh'").Scan(&v) == nil
}

// scan walks the corpus and loads the files table.
func (ix *Index) scan(ctx context.Context) ([]corpus.File, map[string]fileRow, error) {
	var (
		files   []corpus.File
		walkErr error
		wg      sync.WaitGroup
	)
	wg.Go(func() { files, walkErr = corpus.Walk(ix.inst.Root, ix.x.layout) })
	known, err := ix.loadFiles(ctx)
	wg.Wait()
	if walkErr != nil {
		return nil, nil, fmt.Errorf("walk corpus: %w", walkErr)
	}
	return files, known, err
}

func (ix *Index) loadFiles(ctx context.Context) (map[string]fileRow, error) {
	rows, err := ix.db.QueryContext(ctx, "SELECT path, size, mtime_ns, hash FROM files")
	if err != nil {
		return nil, fmt.Errorf("load files: %w", err)
	}
	defer func() { _ = rows.Close() }()
	known := make(map[string]fileRow, 1024)
	for rows.Next() {
		var p string
		var r fileRow
		if err := rows.Scan(&p, &r.size, &r.mtime, &r.hash); err != nil {
			return nil, fmt.Errorf("load files: %w", err)
		}
		known[p] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load files: %w", err)
	}
	return known, nil
}

type refreshPlan struct {
	candidates []corpus.File // new or stat-changed
	removed    []string
}

func (p refreshPlan) empty() bool { return len(p.candidates) == 0 && len(p.removed) == 0 }

func diff(files []corpus.File, known map[string]fileRow) refreshPlan {
	var p refreshPlan
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		seen[f.Rel] = true
		k, ok := known[f.Rel]
		if !ok || k.size != f.Size || k.mtime != f.ModNS {
			p.candidates = append(p.candidates, f)
		}
	}
	for path := range known {
		if !seen[path] {
			p.removed = append(p.removed, path)
		}
	}
	return p
}

// refreshLocked rescans under the lock and applies the changes.
func (ix *Index) refreshLocked(ctx context.Context) (Stats, error) {
	start := time.Now()
	files, known, err := ix.scan(ctx)
	if err != nil {
		return Stats{}, err
	}
	st, err := ix.apply(ctx, files, known)
	st.Elapsed = time.Since(start)
	return st, err
}

// apply indexes the difference between files and known in one transaction.
func (ix *Index) apply(ctx context.Context, files []corpus.File, known map[string]fileRow) (Stats, error) {
	plan := diff(files, known)
	st := Stats{Files: len(files), Removed: len(plan.removed)}
	changes := ix.readChanges(plan.candidates)
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return st, fmt.Errorf("begin refresh: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	w, err := newWriter(ctx, tx)
	if err != nil {
		return st, err
	}
	defer w.close()
	for _, path := range plan.removed {
		if err := w.deletePath(ctx, path); err != nil {
			return st, err
		}
	}
	if err := w.applyChanges(ctx, changes, known, &st); err != nil {
		return st, err
	}
	if err := w.setMeta(ctx, "last_refresh", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return st, err
	}
	if err := tx.Commit(); err != nil {
		return st, fmt.Errorf("commit refresh: %w", err)
	}
	return st, nil
}

// applyChanges re-indexes changed files and records mtime-only touches.
func (w *writer) applyChanges(ctx context.Context, changes []change, known map[string]fileRow, st *Stats) error {
	for _, c := range changes {
		if c.err != nil {
			continue // vanished between walk and read; next refresh removes it
		}
		k, existed := known[c.file.Rel]
		if existed && bytes.Equal(k.hash, c.hash) {
			if err := w.touch(ctx, c); err != nil {
				return err
			}
			continue
		}
		if existed {
			st.Changed++
		} else {
			st.Added++
		}
		if err := w.replace(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// readChanges reads, hashes and extracts candidate files in parallel
// (extracting eagerly: a stat change is almost always a content change).
func (ix *Index) readChanges(files []corpus.File) []change {
	out := make([]change, len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), max(1, len(files))) {
		wg.Go(func() {
			for i := range next {
				f := files[i]
				raw, err := os.ReadFile(f.Abs)
				if err != nil {
					out[i] = change{file: f, err: err}
					continue
				}
				hb := binary.BigEndian.AppendUint64(nil, xxh3.Hash(raw))
				out[i] = change{file: f, hash: hb, recs: ix.x.extract(f, raw)}
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	return out
}

// isUnique reports a UNIQUE constraint violation.
func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
