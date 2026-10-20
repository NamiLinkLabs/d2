package routing

import "context"

// cachedContextErr avoids repeatedly traversing context value wrappers in hot
// guarded loops. Standard cancellable contexts close Done before Err becomes
// observable; synthetic contexts without Done retain per-call Err polling.
func cachedContextErr(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return ctx.Err()
	}
	select {
	case <-done:
		return ctx.Err()
	default:
		return nil
	}
}

// chargeSkippedRouteWork retains the logical work of a linear scan after its
// irrelevant prefix has been found by binary search. Nonstandard budgets and
// synthetic contexts retain exact step-by-step accounting and cancellation.
func chargeSkippedRouteWork(guard workBudget, amount int) error {
	if guard == nil || amount == 0 {
		return nil
	}
	search, ok := guard.(*routeSearchWorkGuard)
	if !ok || !search.batchesSteps() {
		for range amount {
			if err := guard.step(); err != nil {
				return err
			}
		}
		return nil
	}
	units := uint64(amount)
	if search.used >= search.limit {
		return search.step()
	}
	if remaining := search.limit - search.used; units > remaining {
		if err := search.add(remaining); err != nil {
			return err
		}
		return search.step()
	}
	return search.add(units)
}

// batchesSteps reports whether a run of single steps may be charged as one
// bulk charge with identical accounting: a standard cancellable context (so
// polling every step is not required) and no per-step aggregate budget.
func (guard *routeSearchWorkGuard) batchesSteps() bool {
	return guard.done != nil && (guard.aggregate == nil || guard.deferAggregate)
}

// batchesRouteSteps reports whether chargeSkippedRouteWork charges guard in
// bulk rather than one step at a time.
func batchesRouteSteps(guard workBudget) bool {
	search, ok := guard.(*routeSearchWorkGuard)
	return ok && search.batchesSteps()
}
