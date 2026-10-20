//go:build js || wasip1

package limits

import (
	"context"
	"time"
)

// DeadlineErr returns context.DeadlineExceeded once ctx's deadline has passed,
// even if the context's timer has not closed Done yet. WebAssembly runs Go on
// a single thread without preemption, so timers fire only when the program
// yields to the host; CPU-bound work would otherwise run past its deadline
// until it ends.
func DeadlineErr(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
