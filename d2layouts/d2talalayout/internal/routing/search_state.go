package routing

// crossingMemoEntry is one OVG edge's cached edge-set crossing scan.
type crossingMemoEntry struct {
	epoch      uint32
	intersects bool
	steps      int
}

// clearSearchState zeroes the node-indexed search arrays at every index the
// previous search touched. The previous search wrote nodeContext only at the
// indices it recorded in searchTouched, and wrote hops only at indices whose
// nodeContext it had already set, so afterwards every entry up to the arrays'
// capacity is zero again, exactly as clearing them in full would leave them.
func (router *ovgEdgeRouter) clearSearchState() {
	if len(router.searchTouched) == 0 {
		return
	}
	verticalHops := router.verticalHops[:cap(router.verticalHops)]
	horizontalHops := router.horizontalHops[:cap(router.horizontalHops)]
	nodeContext := router.nodeContext[:cap(router.nodeContext)]
	for _, i := range router.searchTouched {
		verticalHops[i] = nil
		horizontalHops[i] = nil
		nodeContext[i] = nil
	}
	router.searchTouched = router.searchTouched[:0]
}

// beginCrossingMemo starts a new search epoch for searchCrossing.
func (router *ovgEdgeRouter) beginCrossingMemo() {
	if edgeCount := len(router.ovg.Edges); len(router.crossingMemo) < edgeCount {
		router.crossingMemo = append(router.crossingMemo, make([]crossingMemoEntry, edgeCount-len(router.crossingMemo))...)
	}
	router.crossingMemoEpoch++
	if router.crossingMemoEpoch == 0 {
		clear(router.crossingMemo)
		router.crossingMemoEpoch = 1
	}
}

// searchCrossing is router.edgeSet.intersectsWithGuarded(e, router.work) for
// an OVG edge relaxed by the current search.
//
// The edge set only changes in addRoute, between searches, and an OVG edge's
// endpoints never change, so within one search every scan of the same edge
// returns the same answer after the same number of steps. A search relaxes
// most edges more than once. When the guard charges steps in bulk,
// intersectsWithChecked scans without a budget and then charges the scan's
// step count through chargeSkippedRouteWork; a repeated scan is replaced by
// the cached answer and the same charge, so the answer, the charged work and
// the limit behavior are identical. Guards that must observe every step and
// edges that are not this OVG's keep the original path.
func (router *ovgEdgeRouter) searchCrossing(e *OVGEdge) (bool, error) {
	i := e.index
	if uint(i) >= uint(len(router.crossingMemo)) || i >= len(router.ovg.Edges) || router.ovg.Edges[i] != e ||
		!batchesRouteSteps(router.work) {
		return router.edgeSet.intersectsWithGuarded(e, router.work)
	}
	entry := &router.crossingMemo[i]
	if entry.epoch != router.crossingMemoEpoch {
		entry.intersects, entry.steps = router.edgeSet.intersectsWithCount(e)
		entry.epoch = router.crossingMemoEpoch
	}
	if err := chargeSkippedRouteWork(router.work, entry.steps); err != nil {
		return false, err
	}
	return entry.intersects, nil
}
