package crossgaps

import (
	"context"

	"github.com/d2lang/d2/d2target"
)

// FindWithWork is Find with a different bound on the segment pairs compared.
func FindWithWork(ctx context.Context, diagram *d2target.Diagram, opts Options, work int) (map[string][]Gap, error) {
	return find(ctx, diagram, opts, work)
}
