package d2talalayout

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/routing"
	"github.com/d2lang/d2/lib/geo"
)

// MaxSearchWorkUnits is RouteEdges' route-search budget for each route
// flavor, in work units.
const MaxSearchWorkUnits = routing.MaxSearchWorkUnits

const (
	// maxEdgesRoutedTogether is the most edges RouteEdgesBestEffort routes in
	// one call. Routing edges together costs superlinearly more than routing
	// them one pair at a time.
	maxEdgesRoutedTogether = 10
	// defaultPairWork is each route flavor's default budget for one pair.
	// One pair alone on a graph of 150 shapes takes well under 2 million.
	defaultPairWork = 5_000_000
	// defaultTotalPairWork is the default budget for all pairs: about a second
	// on one core.
	defaultTotalPairWork = 100_000_000
	// pairShapeWork is what each attempt at a pair is charged per shape for
	// building TALA's routing graph, on top of its route search, without a
	// WorkBudget.
	pairShapeWork = 5_000
	// attemptShapeWork is what a WorkBudget is charged per shape for each
	// attempt, for translating the graph to TALA's and back, on top of the
	// measured work of building and searching the routing graph.
	attemptShapeWork = 500
	// ovgWorkWeight is what a WorkBudget is charged for each unit of work
	// building a routing graph: such a unit takes about three times as long as
	// one of route search.
	ovgWorkWeight = 3
	// labelWorkWeight is what a WorkBudget is charged for each unit of work
	// placing edge labels.
	labelWorkWeight = 2
	// togetherShapeWork times the square of the shape count is about the
	// least that routing edges together costs.
	togetherShapeWork = 500
	// Routing edges together may use at most this many quarters of what is
	// left of a WorkBudget, so that, if it runs out, the pairs get the rest.
	togetherQuarters = 3
)

// BestEffortOptions sets RouteEdgesBestEffort's work budgets. They count
// route-search steps, in the units of MaxSearchWorkUnits, rather than time,
// so the routes do not depend on the machine. Zero fields use the defaults.
type BestEffortOptions struct {
	// TogetherWork is each route flavor's budget for routing up to 10 edges
	// together [default: MaxSearchWorkUnits, as in RouteEdges].
	TogetherWork uint64
	// PairWork is each route flavor's budget for routing the edges between
	// one pair of shapes [default: 5,000,000].
	PairWork uint64
	// TotalPairWork is the budget for routing pairs, summed over pairs and
	// route flavors, plus 5,000 per shape for each pair. The pairs left when
	// it runs out are not routed [default: 100,000,000]. It applies only
	// without a Budget.
	TotalPairWork uint64
	// Prefer, if set, picks the edges to route first when routing pair by
	// pair: the pairs with such an edge go before the others, so that a
	// Budget is spent on them first. Pairs are routed independently of each
	// other, so their order changes no route.
	Prefer func(*d2graph.Edge) bool
	// Budget, if set, is the budget for all the routing this call does,
	// together and pair by pair, and replaces TotalPairWork. It can be shared
	// by several calls: each call spends from what the previous ones left,
	// and once it is spent the edges left are not routed.
	Budget *WorkBudget
	// TALA, if set, holds the options of the TALA layout the edges belong
	// to. Of them only SeparateEdges applies, and diagram data under
	// tala-separate-edges takes precedence, as for Layout. Without it the
	// edges are bundled and that data is ignored, as layouts other than
	// TALA ignore it.
	TALA *Options
}

// WorkBudget is a budget of routing work shared by RouteEdgesBestEffort
// calls, in the units of MaxSearchWorkUnits. It counts the work of searching
// TALA's routing graph, three units for each unit of work building it, and
// 500 units per shape for each attempt, but not time, so how much gets routed
// depends only on the diagram, never on the machine or its load. A unit
// takes about 15 ns on one core. A WorkBudget must not be used by concurrent
// calls.
type WorkBudget struct {
	limit, spent uint64
}

// NewWorkBudget returns a budget of units work units.
func NewWorkBudget(units uint64) *WorkBudget {
	return &WorkBudget{limit: units}
}

// Spent returns the work spent so far. A routing attempt starts only while
// some budget is left, and building and searching its routing graph stop at
// what is left, so Spent exceeds the budget by at most the label placement
// and per-shape work of the last attempt.
func (b *WorkBudget) Spent() uint64 {
	return b.spent
}

// Remaining returns the work left to spend.
func (b *WorkBudget) Remaining() uint64 {
	if b.spent >= b.limit {
		return 0
	}
	return b.limit - b.spent
}

func (b *WorkBudget) charge(units uint64) {
	if b.spent > math.MaxUint64-units {
		b.spent = math.MaxUint64
		return
	}
	b.spent += units
}

// attemptBudget returns the most work an attempt may do with quarters
// quarters of what is left of b, and whether anything is left at all.
func (b *WorkBudget) attemptBudget(quarters uint64) (uint64, bool) {
	remaining := b.Remaining()
	if remaining == 0 {
		return 0, false
	}
	return max(remaining/4*quarters, 1), true
}

// chargeAttempt charges b for an attempt on g whose work telemetry recorded.
func (b *WorkBudget) chargeAttempt(g *d2graph.Graph, telemetry *routing.SearchTelemetry) {
	b.charge(attemptShapeWork * uint64(len(g.Objects)))
	b.charge(ovgWorkWeight * telemetry.OVGWork())
	b.charge(labelWorkWeight * telemetry.LabelWork())
	for _, work := range telemetry.WorkSamples() {
		b.charge(work)
	}
}

func (o *BestEffortOptions) withDefaults() BestEffortOptions {
	var opts BestEffortOptions
	if o != nil {
		opts = *o
	}
	if opts.TogetherWork == 0 {
		opts.TogetherWork = MaxSearchWorkUnits
	}
	if opts.PairWork == 0 {
		opts.PairWork = defaultPairWork
	}
	if opts.TotalPairWork == 0 {
		opts.TotalPairWork = defaultTotalPairWork
	}
	return opts
}

// RouteEdgesBestEffort routes edges with TALA's edge router around the shapes
// already placed in g, like RouteEdges, but leaves the edges it cannot route
// alone instead of failing. It returns those edges, in their order in edges,
// with the routes they had before the call. opts may be nil.
//
// Up to 10 edges are first routed together around the other edges' routes,
// as RouteEdges does. If there are more, or that runs out of budget, the
// edges are routed one pair of shapes at a time, ignoring the other edges, so
// edges between the same two shapes still get distinct routes. Connections
// from a shape to itself are never routed.
//
// With opts.Budget, every attempt draws from that budget and stops when it
// runs out. Routing together is skipped when the budget plainly cannot pay
// for it, and the pairs are routed until the budget is spent. The work
// counted for a graph is the same on every run, so the same input always
// leaves the same edges unrouted.
//
// Only a nil context or graph, an edge that is nil, repeated or not in g, and
// the context's cancellation are returned as errors. RouteEdgesBestEffort
// mutates g, restores the order of g.Edges, and must not run concurrently
// against the same graph.
func RouteEdgesBestEffort(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge, opts *BestEffortOptions) (unrouted []*d2graph.Edge, err error) {
	budget := opts.withDefaults()
	var separate separateEdgesFunc
	if budget.TALA != nil {
		separate = talaSeparateEdges(budget.TALA)
	}
	if ctx == nil {
		return nil, fmt.Errorf("TALA edge routing requires a context")
	}
	if g == nil || g.Root == nil {
		return nil, fmt.Errorf("tala requires a D2 graph with a root object")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRequestedD2Edges(g, edges); err != nil {
		return nil, err
	}
	if len(edges) == 0 {
		return nil, nil
	}

	all := g.Edges
	defer func() { g.Edges = all }()

	selected := make(map[*d2graph.Edge]bool, len(edges))
	routable := make([]*d2graph.Edge, 0, len(edges))
	previous := make(map[*d2graph.Edge]edgeRouteState, len(edges))
	for _, e := range edges {
		selected[e] = true
		if e.Src == e.Dst {
			continue
		}
		routable = append(routable, e)
		previous[e] = saveEdgeRouteState(e)
		// TALA validates every route in the graph, and the ones it is asked to
		// replace may be placeholders, such as the zero-length stub between two
		// touching grid cells.
		e.Route = nil
		e.IsCurve = false
	}
	routed := make(map[*d2graph.Edge]bool, len(routable))
	defer func() {
		for _, e := range edges {
			if routed[e] {
				continue
			}
			if state, ok := previous[e]; ok {
				state.restore(e)
			}
			if err == nil {
				unrouted = append(unrouted, e)
			}
		}
	}()

	if len(routable) > 0 && len(routable) <= maxEdgesRoutedTogether {
		// Route around the edges that already have a usable route; TALA needs a
		// complete route on every edge it is not asked to route.
		g.Edges = make([]*d2graph.Edge, 0, len(all))
		for _, e := range all {
			if selected[e] && e.Src != e.Dst || !selected[e] && hasRoutableRoute(e) {
				g.Edges = append(g.Edges, e)
			}
		}
		attemptWork, ok := uint64(0), true
		if budget.Budget != nil {
			attemptWork, ok = budget.Budget.attemptBudget(togetherQuarters)
			// Routing edges together around the routes of the others costs
			// about the square of the shape count. Skip it, rather than spend
			// the budget failing, when it is plainly out of reach.
			n := uint64(len(g.Objects))
			ok = ok && togetherShapeWork*n*n <= attemptWork
		}
		routeErr := errors.New("routing work budget spent")
		if ok {
			telemetry := &routing.SearchTelemetry{}
			routeErr = routeEdgesWithinBudget(routing.WithSearchTelemetry(ctx, telemetry), g, routable, separate, budget.TogetherWork, attemptWork)
			if budget.Budget != nil {
				budget.Budget.chargeAttempt(g, telemetry)
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if routeErr == nil {
			for _, e := range routable {
				routed[e] = true
			}
			return nil, nil
		}
	}

	// A failed attempt leaves the graph as it was, so each pair starts from
	// the cleared routes.
	spent := uint64(0)
	pairs := groupByEndpoints(routable)
	if budget.Prefer != nil {
		var preferred, others [][]*d2graph.Edge
		for _, pair := range pairs {
			if slices.ContainsFunc(pair, budget.Prefer) {
				preferred = append(preferred, pair)
			} else {
				others = append(others, pair)
			}
		}
		pairs = append(preferred, others...)
	}
	for _, pair := range pairs {
		attemptWork := uint64(0)
		if budget.Budget != nil {
			var ok bool
			if attemptWork, ok = budget.Budget.attemptBudget(4); !ok {
				break
			}
		} else if spent >= budget.TotalPairWork {
			break
		}
		g.Edges = pair
		spent += pairShapeWork * uint64(len(g.Objects))
		telemetry := &routing.SearchTelemetry{}
		routeErr := routeEdgesWithinBudget(routing.WithSearchTelemetry(ctx, telemetry), g, pair, separate, budget.PairWork, attemptWork)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, work := range telemetry.WorkSamples() {
			spent += work
		}
		if budget.Budget != nil {
			budget.Budget.chargeAttempt(g, telemetry)
		}
		if routeErr == nil {
			for _, e := range pair {
				routed[e] = true
			}
		}
	}
	return nil, nil
}

// groupByEndpoints groups edges by their unordered pair of endpoints, in the
// order each pair first appears.
func groupByEndpoints(edges []*d2graph.Edge) [][]*d2graph.Edge {
	type endpoints struct{ a, b *d2graph.Object }
	index := make(map[endpoints]int, len(edges))
	var groups [][]*d2graph.Edge
	for _, e := range edges {
		key := endpoints{e.Src, e.Dst}
		if i, ok := index[endpoints{e.Dst, e.Src}]; ok {
			groups[i] = append(groups[i], e)
			continue
		}
		if i, ok := index[key]; ok {
			groups[i] = append(groups[i], e)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, []*d2graph.Edge{e})
	}
	return groups
}

// hasRoutableRoute reports whether TALA accepts e's route as one to route
// around.
func hasRoutableRoute(e *d2graph.Edge) bool {
	if len(e.Route) < 2 {
		return false
	}
	for i, p := range e.Route {
		if p == nil || validateResultCoordinate("route x", p.X) != nil || validateResultCoordinate("route y", p.Y) != nil {
			return false
		}
		if i > 0 && p.X == e.Route[i-1].X && p.Y == e.Route[i-1].Y {
			return false
		}
	}
	return true
}

// edgeRouteState is the part of an edge that routing replaces.
type edgeRouteState struct {
	route           []*geo.Point
	isCurve         bool
	labelPosition   *string
	labelPercentage *float64
}

func saveEdgeRouteState(e *d2graph.Edge) edgeRouteState {
	return edgeRouteState{
		route:           e.Route,
		isCurve:         e.IsCurve,
		labelPosition:   e.LabelPosition,
		labelPercentage: e.LabelPercentage,
	}
}

func (s edgeRouteState) restore(e *d2graph.Edge) {
	e.Route = s.route
	e.IsCurve = s.isCurve
	e.LabelPosition = s.labelPosition
	e.LabelPercentage = s.labelPercentage
}
