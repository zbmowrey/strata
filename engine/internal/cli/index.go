package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/render"
)

// openIndex opens the instance and its (fresh) index.
func (a *app) openIndex(ctx context.Context) (*config.Instance, *index.Index, error) {
	inst, err := a.instance()
	if err != nil {
		return nil, nil, err
	}
	ix, st, err := index.Open(ctx, inst, index.Options{NoRefresh: a.g.noRefresh, StrictFresh: a.g.strictFresh})
	if err != nil {
		return nil, nil, err
	}
	if a.g.verbose {
		a.warnf("index: %d files, +%d ~%d -%d in %s (events: %v)", st.Files, st.Added, st.Changed, st.Removed, st.Elapsed.Round(time.Microsecond), ix.Events)
	}
	if st.Skipped && !a.g.noRefresh {
		a.warnf("index is being refreshed by another process; results may be slightly stale (use --strict-fresh to wait)")
	}
	return inst, ix, nil
}

// width is the line-format width for this invocation.
func (a *app) width() int { return render.Width(a.io.OutFd, a.io.OutTTY) }

func (a *app) printf(format string, args ...any) error {
	_, err := fmt.Fprintf(a.io.Out, format, args...)
	return err
}
