package d2layouts

import (
	"context"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
)

// DefaultRouter's routing work in one diagram is bounded by a budget that
// grows with the diagram, in d2talalayout.WorkBudget's units: about 15 ns
// each on one core and three to five times that in WebAssembly. Routing the
// connections that cross into and out of grids, near shapes and sequence
// diagrams in a diagram of up to about 40 shapes takes up to about 2 million,
// which the base covers, so such diagrams route fully, for about 0.1 s more
// in WebAssembly. A pair of shapes in a diagram of 150 shapes takes about a
// million, so big diagrams route some of theirs and draw the rest straight:
// this keeps their compile time within about twice master's. The budget
// counts work, not time, so the same diagram always routes the same edges,
// on any machine.
var (
	// routeWorkBase is the budget of every diagram.
	routeWorkBase uint64 = 2_500_000
	// routeWorkPerShape is added to the budget for each shape in the diagram.
	routeWorkPerShape uint64 = 12_000
)

// routeWorkBudget returns the routing budget of a diagram of shapes shapes.
func routeWorkBudget(shapes int) uint64 {
	return routeWorkBase + routeWorkPerShape*uint64(shapes)
}

type routingRunKey struct{}

// routingRun is the routing of one LayoutNested call, nested diagrams
// included, which DefaultRouter's calls share one budget across.
type routingRun struct {
	shapes int
	budget *d2talalayout.WorkBudget
}

// withRoutingRun returns ctx carrying a routing run for g, the whole diagram,
// unless it carries one already.
func withRoutingRun(ctx context.Context, g *d2graph.Graph) context.Context {
	if _, ok := ctx.Value(routingRunKey{}).(*routingRun); ok {
		return ctx
	}
	return context.WithValue(ctx, routingRunKey{}, &routingRun{shapes: len(g.Objects)})
}

// routeBudget returns the budget DefaultRouter spends routing edges in g:
// the one of the LayoutNested call it routes for, or one for g alone when it
// is called on its own.
func routeBudget(ctx context.Context, g *d2graph.Graph) *d2talalayout.WorkBudget {
	run, ok := ctx.Value(routingRunKey{}).(*routingRun)
	if !ok {
		return d2talalayout.NewWorkBudget(routeWorkBudget(len(g.Objects)))
	}
	if run.budget == nil {
		run.budget = d2talalayout.NewWorkBudget(routeWorkBudget(run.shapes))
	}
	return run.budget
}
