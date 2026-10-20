package d2talalayout

import (
	"context"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// D2's top and left are absolute diagram coordinates at every nesting level.
// The engine's FixedTopLeft is absolute only for root-level nodes: a pinned
// node inside a container is placed relative to that container's interior
// origin, i.e. the container's top-left plus its padding, and the container is
// sized so that this origin is its first content point. Taken literally, a pin
// such as g.q: {top: 500; left: 50} therefore lands at g's top-left plus
// (60, 60) plus (50, 500), with an empty band above and left of q.
//
// absolutePins translates D2 pins into engine pins before layout:
//
//   - Every unpinned container with a pinned descendant becomes implicitly
//     pinned. Its interior origin is placed at the component-wise minimum of its
//     pinned children's targets, so the container hugs them from the top left.
//   - A pinned child of a container is pinned relative to that container's
//     interior origin, preserving its absolute target.
//
// The interior offset of a container depends on its label, icon and shape, and
// is only final once its children are placed. absolutePins assumes the
// default container padding, verifies the selected layout, and reports the
// offsets it measured so that Layout can run once more with exact values.
type absolutePins struct {
	graph *layoutgraph.Graph
	// targets are the absolute top-left targets of explicitly pinned nodes.
	targets map[*layoutgraph.Node]geo.Point
	// offsets are the assumed interior offsets of containers with pinned
	// children.
	offsets map[*layoutgraph.Node]geo.Point
	// relative are the engine pins of nested pinned nodes, by node.
	relative map[*layoutgraph.Node]geo.Point
}

const pinTolerance = 0.5

// newAbsolutePins records the absolute pins already translated onto graph and
// returns nil when no pinned node is nested in a container, in which case the
// engine semantics already match D2's.
func newAbsolutePins(graph *layoutgraph.Graph) *absolutePins {
	plan := &absolutePins{
		graph:   graph,
		targets: make(map[*layoutgraph.Node]geo.Point),
		offsets: make(map[*layoutgraph.Node]geo.Point),
	}
	nested := false
	for _, node := range graph.Nodes {
		if node.FixedTopLeft == nil {
			continue
		}
		plan.targets[node] = *node.FixedTopLeft
		if node.Container != nil {
			nested = true
		}
	}
	if !nested {
		return nil
	}
	plan.apply()
	return plan
}

func defaultInteriorOffset() geo.Point {
	return geo.Point{X: layoutgraph.ContainerPadding, Y: layoutgraph.ContainerPadding}
}

func (plan *absolutePins) offset(container *layoutgraph.Node) geo.Point {
	if offset, ok := plan.offsets[container]; ok {
		return offset
	}
	return defaultInteriorOffset()
}

// apply rewrites FixedTopLeft for every pinned and implicitly pinned node.
func (plan *absolutePins) apply() {
	g := plan.graph
	targets := make(map[*layoutgraph.Node]geo.Point, len(plan.targets))
	// Post-order: a container's target depends on its children's targets.
	var visit func(node *layoutgraph.Node) bool
	visit = func(node *layoutgraph.Node) bool {
		pinnedChild := false
		minTarget := geo.Point{X: math.Inf(1), Y: math.Inf(1)}
		for _, child := range g.Containers[node] {
			if !visit(child) {
				continue
			}
			pinnedChild = true
			minTarget.X = math.Min(minTarget.X, targets[child].X)
			minTarget.Y = math.Min(minTarget.Y, targets[child].Y)
		}
		if node == nil {
			return pinnedChild
		}
		if target, explicit := plan.targets[node]; explicit {
			targets[node] = target
			return true
		}
		if !pinnedChild {
			return false
		}
		offset := plan.offset(node)
		targets[node] = geo.Point{X: minTarget.X - offset.X, Y: minTarget.Y - offset.Y}
		return true
	}
	visit(nil)

	plan.relative = make(map[*layoutgraph.Node]geo.Point, len(targets))
	for _, node := range g.Nodes {
		target, pinned := targets[node]
		if !pinned {
			continue
		}
		if node.Container == nil {
			node.FixedTopLeft = geo.NewPoint(target.X, target.Y)
			continue
		}
		parentTarget := targets[node.Container]
		offset := plan.offset(node.Container)
		// An explicitly pinned container can conflict with a child pinned above
		// or left of its interior. The container pin wins; the child is kept at
		// the nearest point inside it.
		relative := geo.Point{
			X: math.Max(0, target.X-parentTarget.X-offset.X),
			Y: math.Max(0, target.Y-parentTarget.Y-offset.Y),
		}
		plan.relative[node] = relative
		node.FixedTopLeft = geo.NewPoint(relative.X, relative.Y)
	}
}

// correct compares the selected layout with the absolute targets. It returns
// true, after updating the engine pins, when a container's real interior
// offset differs from the assumed one, so another layout pass would place the
// pins exactly.
func (plan *absolutePins) correct(ctx context.Context, result *layoutgraph.Graph) bool {
	if plan == nil || result == nil || ctx.Err() != nil {
		return false
	}
	byID := make(map[layoutgraph.EntityID]*layoutgraph.Node, len(result.Nodes))
	for _, node := range result.Nodes {
		byID[node.ID] = node
	}
	exact := true
	for node, target := range plan.targets {
		placed := byID[node.ID]
		if placed == nil || placed.TopLeft == nil ||
			math.Abs(placed.TopLeft.X-target.X) > pinTolerance || math.Abs(placed.TopLeft.Y-target.Y) > pinTolerance {
			exact = false
			break
		}
	}
	if exact {
		return false
	}

	changed := false
	measuredContainers := make(map[*layoutgraph.Node]struct{})
	// Iterate in graph order so the measured offset does not depend on map
	// order. Every pinned child of a container measures the same offset.
	for _, node := range plan.graph.Nodes {
		relative, nested := plan.relative[node]
		if !nested {
			continue
		}
		container := node.Container
		if _, done := measuredContainers[container]; done {
			continue
		}
		measuredContainers[container] = struct{}{}
		placed, placedContainer := byID[node.ID], byID[container.ID]
		if placed == nil || placedContainer == nil || placed.TopLeft == nil || placedContainer.TopLeft == nil {
			continue
		}
		measured := geo.Point{
			X: placed.TopLeft.X - placedContainer.TopLeft.X - relative.X,
			Y: placed.TopLeft.Y - placedContainer.TopLeft.Y - relative.Y,
		}
		assumed := plan.offset(container)
		if math.Abs(measured.X-assumed.X) > pinTolerance || math.Abs(measured.Y-assumed.Y) > pinTolerance {
			plan.offsets[container] = measured
			changed = true
		}
	}
	if changed {
		plan.apply()
	}
	return changed
}
