package layoutgraph

import "context"

// RefinementBudget meters optional placement refinement (swaps, transposes,
// axis alignment, gap normalization and similar passes) so a very large graph
// stops refining and keeps its best valid placement instead of exhausting the
// aggregate transaction guard and failing the whole layout.
//
// The meter is deliberately not derived from limits.WorkGuard counts. Those
// counts include map-ordered scans that exit early, so two runs of the same
// layout can charge slightly different totals. A decision taken on such a
// count would make the layout depend on map iteration order. The meter only
// counts quantities fixed by the deterministic sequence of transaction
// commits: the operations and graph nodes each commit validates and the
// overlap-candidate references it builds.
//
// One budget belongs to one pipeline run and is used from that run's goroutine
// only. A stage observes exhaustion through RefinementExhausted at points
// where stopping leaves a complete, valid layout.
type RefinementBudget struct {
	totalLimit int64
	stageLimit int64
	totalUsed  int64
	stageUsed  int64
	exhausted  bool
}

// NewRefinementBudget returns a meter with an aggregate limit for every
// metered stage of one pipeline run and a limit for each individual stage, so
// one expensive stage cannot starve the later, cheaper ones. Non-positive
// limits are exhausted immediately.
func NewRefinementBudget(totalLimit, stageLimit int64) *RefinementBudget {
	return &RefinementBudget{totalLimit: totalLimit, stageLimit: stageLimit}
}

// BeginStage starts metering a new stage. The aggregate use is retained.
func (budget *RefinementBudget) BeginStage() {
	if budget == nil {
		return
	}
	budget.stageUsed = 0
	budget.exhausted = budget.totalUsed >= budget.totalLimit || budget.stageLimit <= 0
}

func (budget *RefinementBudget) charge(units int64) {
	if budget == nil || units <= 0 {
		return
	}
	budget.totalUsed = saturatingAdd(budget.totalUsed, units)
	budget.stageUsed = saturatingAdd(budget.stageUsed, units)
	if budget.totalUsed >= budget.totalLimit || budget.stageUsed >= budget.stageLimit {
		budget.exhausted = true
	}
}

// Exhausted reports whether the current stage should stop refining.
func (budget *RefinementBudget) Exhausted() bool {
	return budget != nil && budget.exhausted
}

// TotalExhausted reports whether every remaining metered stage should be
// skipped.
func (budget *RefinementBudget) TotalExhausted() bool {
	return budget != nil && budget.totalUsed >= budget.totalLimit
}

// Used returns the aggregate metered units.
func (budget *RefinementBudget) Used() int64 {
	if budget == nil {
		return 0
	}
	return budget.totalUsed
}

func saturatingAdd(value, units int64) int64 {
	if value > (1<<63-1)-units {
		return 1<<63 - 1
	}
	return value + units
}

type refinementBudgetKey struct{}

// ContextWithRefinementBudget meters every transaction committed with the
// returned context, including nested trial transactions.
func ContextWithRefinementBudget(ctx context.Context, budget *RefinementBudget) context.Context {
	if ctx == nil || budget == nil {
		return ctx
	}
	return context.WithValue(ctx, refinementBudgetKey{}, budget)
}

func refinementBudgetFromContext(ctx context.Context) *RefinementBudget {
	if ctx == nil {
		return nil
	}
	budget, _ := ctx.Value(refinementBudgetKey{}).(*RefinementBudget)
	return budget
}

// RefinementExhausted reports whether the stage running with ctx has used its
// refinement budget. Stages poll it between complete candidates; it is always
// false outside a metered stage.
func RefinementExhausted(ctx context.Context) bool {
	return refinementBudgetFromContext(ctx).Exhausted()
}
