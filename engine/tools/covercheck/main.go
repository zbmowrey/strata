// Command covercheck enforces a per-package statement-coverage floor over a Go
// cover profile (docs/spec/06-gates.md §9: ≥ 80% per internal/ package).
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

type counts struct{ total, covered int }

func main() {
	minPct := flag.Float64("min", 80, "minimum coverage percent per package")
	prefix := flag.String("prefix", "github.com/zbmowrey/strata/engine/internal/", "only packages with this import-path prefix are checked")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: covercheck [-min N] [-prefix P] coverage.out")
		os.Exit(2)
	}
	pkgs, err := parseFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "covercheck:", err)
		os.Exit(2)
	}
	if failed := report(os.Stdout, pkgs, *prefix, *minPct); failed > 0 {
		fmt.Fprintf(os.Stderr, "covercheck: %d package(s) below %.0f%% coverage\n", failed, *minPct)
		os.Exit(1)
	}
}

func parseFile(name string) (map[string]counts, error) {
	f, err := os.Open(name) //nolint:gosec // G304: the profile path is a deliberate CLI argument
	if err != nil {
		return nil, fmt.Errorf("open profile: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parse(f)
}

// parse reads a cover profile and aggregates statements per package. Blocks
// listed more than once (atomic/count modes across test binaries) are merged.
func parse(r io.Reader) (map[string]counts, error) {
	type block struct {
		pkg   string
		stmts int
		hit   bool
	}
	blocks := map[string]*block{}
	sc := bufio.NewScanner(r)
	for first := true; sc.Scan(); first = false {
		line := sc.Text()
		if first && strings.HasPrefix(line, "mode:") {
			continue
		}
		key, rest, ok := strings.Cut(line, " ")
		fields := strings.Fields(rest)
		if !ok || len(fields) != 2 {
			return nil, fmt.Errorf("malformed line %q", line)
		}
		stmts, err1 := strconv.Atoi(fields[0])
		hits, err2 := strconv.Atoi(fields[1])
		if err := errors.Join(err1, err2); err != nil {
			return nil, fmt.Errorf("malformed line %q: %w", line, err)
		}
		file, _, _ := strings.Cut(key, ":")
		b, seen := blocks[key]
		if !seen {
			b = &block{pkg: path.Dir(file), stmts: stmts}
			blocks[key] = b
		}
		b.hit = b.hit || hits > 0
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	out := map[string]counts{}
	for _, b := range blocks {
		c := out[b.pkg]
		c.total += b.stmts
		if b.hit {
			c.covered += b.stmts
		}
		out[b.pkg] = c
	}
	return out, nil
}

// report prints coverage for packages under prefix and returns how many are
// below minPct.
func report(w io.Writer, pkgs map[string]counts, prefix string, minPct float64) int {
	names := make([]string, 0, len(pkgs))
	for p := range pkgs {
		if strings.HasPrefix(p, prefix) {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	failed := 0
	for _, p := range names {
		c := pkgs[p]
		pct := 100.0
		if c.total > 0 {
			pct = 100 * float64(c.covered) / float64(c.total)
		}
		mark := "ok  "
		if pct < minPct {
			mark = "FAIL"
			failed++
		}
		_, _ = fmt.Fprintf(w, "%s %6.1f%%  %s\n", mark, pct, strings.TrimPrefix(p, prefix))
	}
	return failed
}
