package engine

import (
	"context"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// WorkLimits overrides the deterministic work budgets at which a layout
// attempt stops optional work and degrades instead of failing. Zero fields
// select the production budgets. TALA's tests install small values with
// ContextWithWorkLimits to force every degradation path; production callers
// never set them.
type WorkLimits struct {
	// RefinementTotal and RefinementStage bound optional placement
	// refinement (see layoutgraph.RefinementBudget).
	RefinementTotal int64
	RefinementStage int64
}

type workLimitsKey struct{}

// ContextWithWorkLimits returns a context whose layout attempts use limits.
func ContextWithWorkLimits(ctx context.Context, limits WorkLimits) context.Context {
	return context.WithValue(ctx, workLimitsKey{}, limits)
}

func workLimitsFromContext(ctx context.Context) WorkLimits {
	if ctx == nil {
		return WorkLimits{}
	}
	limits, _ := ctx.Value(workLimitsKey{}).(WorkLimits)
	return limits
}

func (l WorkLimits) refinementBudget() *layoutgraph.RefinementBudget {
	total, stage := l.RefinementTotal, l.RefinementStage
	if total == 0 {
		total = limits.MaxRefinementWorkUnits
	}
	if stage == 0 {
		stage = limits.MaxRefinementStageWorkUnits
	}
	return layoutgraph.NewRefinementBudget(total, stage)
}
