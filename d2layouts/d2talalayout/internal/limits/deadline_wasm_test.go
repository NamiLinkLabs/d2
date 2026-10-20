//go:build js || wasip1

package limits

import (
	"context"
	"errors"
	"testing"
	"time"
)

// unfiredDeadlineContext has a deadline in the past whose timer has not run,
// as happens to CPU-bound work on single-threaded js/wasm.
type unfiredDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (ctx unfiredDeadlineContext) Deadline() (time.Time, bool) {
	return ctx.deadline, true
}

func TestWorkGuardObservesPassedDeadlineBeforeDoneCloses(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := unfiredDeadlineContext{Context: parent, deadline: time.Now().Add(-time.Millisecond)}
	if _, err := NewWorkGuard(ctx, "test", 10); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("NewWorkGuard() error = %v, want context.DeadlineExceeded", err)
	}

	ctx.deadline = time.Now().Add(time.Hour)
	guard, err := NewWorkGuard(ctx, "test", 10)
	if err != nil {
		t.Fatalf("NewWorkGuard() error = %v", err)
	}
	if err := DeadlineErr(ctx); err != nil {
		t.Fatalf("DeadlineErr() before the deadline = %v", err)
	}
	guard.ctx = unfiredDeadlineContext{Context: parent, deadline: time.Now().Add(-time.Millisecond)}
	if err := guard.Check(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Check() error = %v, want context.DeadlineExceeded", err)
	}
	if err := DeadlineErr(context.Background()); err != nil {
		t.Fatalf("DeadlineErr() without a deadline = %v", err)
	}
}
