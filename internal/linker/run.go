package linker

import (
	"context"
	"errors"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/store"
)

// Run is the linker hook collect calls after sources finish.
// The linking passes are a later milestone. Run checks that the store can be
// read and leaves work items unchanged, so a collect still completes before
// those passes exist.
func Run(ctx context.Context, st *store.Store, since, until time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if st == nil {
		return errors.New("nil store")
	}
	if since.IsZero() || until.IsZero() || !until.After(since) {
		return errors.New("invalid window")
	}
	if _, err := st.CountEvents(ctx); err != nil {
		return err
	}
	return nil
}
