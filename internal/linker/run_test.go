package linker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestRunReadsStore(t *testing.T) {
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 0, 7)
	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), nil, since, until); err == nil {
		t.Fatal("expected a nil store to fail")
	}
	if err := Run(context.Background(), st, until, since); err == nil {
		t.Fatal("expected a reversed window to fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, st, since, until); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
}
