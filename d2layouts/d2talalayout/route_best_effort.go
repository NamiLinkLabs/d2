package d2talalayout

import (
	"context"
	"fmt"

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
	// building TALA's routing graph, on top of its route search.
	pairShapeWork = 5_000
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
	// it runs out are not routed [default: 100,000,000].
	TotalPairWork uint64
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
// Only a nil context or graph, an edge that is nil, repeated or not in g, and
// the context's cancellation are returned as errors. RouteEdgesBestEffort
// mutates g, restores the order of g.Edges, and must not run concurrently
// against the same graph.
func RouteEdgesBestEffort(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge, opts *BestEffortOptions) (unrouted []*d2graph.Edge, err error) {
	budget := opts.withDefaults()
	if ctx == nil {
		return nil, fmt.Errorf("TALA edge routing requires a context")
	}
	if g == nil {
		return nil, fmt.Errorf("TALA edge routing requires a graph")
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
		routeErr := routeEdges(ctx, g, routable, budget.TogetherWork)
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
	for _, pair := range groupByEndpoints(routable) {
		if spent >= budget.TotalPairWork {
			break
		}
		g.Edges = pair
		spent += pairShapeWork * uint64(len(g.Objects))
		telemetry := &routing.SearchTelemetry{}
		routeErr := routeEdges(routing.WithSearchTelemetry(ctx, telemetry), g, pair, budget.PairWork)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, work := range telemetry.WorkSamples() {
			spent += work
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
