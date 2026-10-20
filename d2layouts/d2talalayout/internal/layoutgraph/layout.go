package layoutgraph

// EdgeAbduction represents the temporary transfer of an edge from a descendant
// container to the ancestor child so node placement can operate on containers.
type EdgeAbduction struct {
	Edge *Edge

	OriginallyFrom *Node
	OriginallyTo   *Node

	CurrentFrom *Node
	CurrentTo   *Node
}

// ClusterRDFSOrder returns cluster vessels in reverse depth-first container order.
func (g *Graph) ClusterRDFSOrder() []*Node {
	order := make([]*Node, 0, len(g.Clusters))

	dfsContainerOrder := append(g.containerRDFSOrder(nil), nil)
	for _, container := range dfsContainerOrder {
		for _, child := range g.Containers[container] {
			if child.isClusterVessel {
				order = append(order, child)
			}
		}
	}

	return order
}

// ClusterOrder returns cluster vessels in stable ID order.
func (g *Graph) ClusterOrder() []*Node {
	order := make([]*Node, 0, len(g.Clusters))
	for node := range g.Clusters {
		order = append(order, node)
	}
	sortNodesByID(order)
	return order
}

// TreeOrder returns tree sentinels in stable ID order.
func (g *Graph) TreeOrder() []*Node {
	order := make([]*Node, 0, len(g.Trees))
	for node := range g.Trees {
		order = append(order, node)
	}
	sortNodesByID(order)
	return order
}

// SequenceOrder returns sequence vessels in stable ID order.
func (g *Graph) SequenceOrder() []*Node {
	order := make([]*Node, 0, len(g.Sequences))
	for node := range g.Sequences {
		order = append(order, node)
	}
	sortNodesByID(order)
	return order
}

func (g *Graph) applyEdgeAbductions(edgeAbductions []*EdgeAbduction) {
	for _, edgeAbduction := range edgeAbductions {
		if edgeAbduction.OriginallyFrom != nil && edgeAbduction.CurrentFrom != nil && edgeAbduction.OriginallyFrom != edgeAbduction.CurrentFrom {
			edgeAbduction.Edge.reconnect(edgeAbduction.CurrentFrom, false)
		}
		if edgeAbduction.OriginallyTo != nil && edgeAbduction.CurrentTo != nil && edgeAbduction.OriginallyTo != edgeAbduction.CurrentTo {
			edgeAbduction.Edge.reconnect(edgeAbduction.CurrentTo, true)
		}
	}
}

func (g *Graph) restoreEdgeAbductions(edgeAbductions []*EdgeAbduction) []*EdgeAbduction {
	nodes := map[*Node]struct{}{}
	var queue []*Node
	queue = append(queue, g.Nodes...)
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		nodes[node] = struct{}{}
		if node.isContainer {
			queue = append(queue, g.Containers[node]...)
		}
		if s, is := g.Sequences[node]; is {
			queue = append(queue, s.Nodes...)
		}
		if node.isClusterVessel {
			queue = append(queue, g.Clusters[node].Nodes...)
		}
	}
	// Assign edges back to children
	var remainingAbductions []*EdgeAbduction
	for _, edgeAbduction := range edgeAbductions {
		_, existsFrom := nodes[edgeAbduction.OriginallyFrom]
		if edgeAbduction.OriginallyFrom == nil {
			_, existsFrom = nodes[edgeAbduction.CurrentFrom]
		}
		_, existsTo := nodes[edgeAbduction.OriginallyTo]
		if edgeAbduction.OriginallyTo == nil {
			_, existsTo = nodes[edgeAbduction.CurrentTo]
		}
		if !existsFrom || !existsTo {
			remainingAbductions = append(remainingAbductions, edgeAbduction)
			continue
		}
		if edgeAbduction.OriginallyFrom != nil {
			// If it is a cluster child, assign back to vessel
			if edgeAbduction.OriginallyFrom.Cluster != nil {
				edgeAbduction.OriginallyFrom = edgeAbduction.OriginallyFrom.Cluster.Vessel
			}
			// If it is a sequence child, assign back to vessel
			if edgeAbduction.OriginallyFrom.Sequence != nil {
				edgeAbduction.OriginallyFrom = edgeAbduction.OriginallyFrom.Sequence.Vessel
			}

			// If simple cluster abduction, edge didn't change
			if edgeAbduction.CurrentFrom != edgeAbduction.OriginallyFrom {
				edgeAbduction.Edge.reconnect(edgeAbduction.OriginallyFrom, false)
			}
		}
		if edgeAbduction.OriginallyTo != nil {
			if edgeAbduction.OriginallyTo.Cluster != nil {
				edgeAbduction.OriginallyTo = edgeAbduction.OriginallyTo.Cluster.Vessel
			}
			if edgeAbduction.OriginallyTo.Sequence != nil {
				edgeAbduction.OriginallyTo = edgeAbduction.OriginallyTo.Sequence.Vessel
			}
			if edgeAbduction.CurrentTo != edgeAbduction.OriginallyTo {
				edgeAbduction.Edge.reconnect(edgeAbduction.OriginallyTo, true)
			}
		}
	}
	return remainingAbductions
}

// Edge abduction turns this graph
// .               ┌───────────────────┐
// .               │                   │
// . ┌───┐         │   ┌──┐            │
// . │   ├─────────┼──►│  ├─────┐      │
// . └───┘         │   └──┘     │      │
// .               │            ▼      │
// .   ┌────┐      │          ┌───┐    │
// .   │    │ ◄────┼──────────┤   │    │
// .   └────┘      │          └───┘    │
// .               │                   │
// .               └───────────────────┘
// . into this
// .               ┌───────────────────┐
// .               │                   │
// . ┌───┐         │   ┌──┐            │
// . │   ├────────►│   │  ├─────┐      │
// . └───┘         │   └──┘     │      │
// .               │            ▼      │
// .   ┌────┐      │          ┌───┐    │
// .   │    │◄─────┤          │   │    │
// .   └────┘      │          └───┘    │
// .               │                   │
// .               └───────────────────┘
// to make node placement work on containers instead of moving around container children
// But we still store data on these edge reassignments so that
// 1. we can revert back after node placement
// 2. we can resolve the true location of nodes within containers for median and edge length calculations
func (fromGraph *Graph) abductEdges(container *Node, toGraph *Graph) []*EdgeAbduction {
	edgeAbductions := []*EdgeAbduction{}
	for _, cluster := range fromGraph.Clusters {
		edgeAbductions = append(edgeAbductions, cluster.EdgeAbductions...)
	}
	for _, sequence := range fromGraph.Sequences {
		edgeAbductions = append(edgeAbductions, sequence.EdgeAbductions...)
	}

	used := make([]bool, len(edgeAbductions))
	containerAbductions := []*EdgeAbduction{}
	// Find the edges which are connected to a descendant at one end and to a child at another end
	for _, edge := range fromGraph.Edges {
		fromAChild := false
		toAChild := false

		fromAChildDescendant := false
		toAChildDescendant := false

		var fromChildAncestor *Node
		var toChildAncestor *Node

		for _, child := range fromGraph.Containers[container] {
			if child == edge.From {
				fromAChild = true
			} else if edge.From.isDescendantOf(child) {
				fromAChildDescendant = true
				fromChildAncestor = child
			}
			if child == edge.To {
				toAChild = true
			} else if edge.To.isDescendantOf(child) {
				toAChildDescendant = true
				toChildAncestor = child
			}
		}

		if (fromAChild && (edge.From == toChildAncestor)) || (toAChild && (edge.To == fromChildAncestor)) {
			// This is an intra-container edge (points from a child into a descendant of child, or vice versa)
			// TODO (Mon Mar 28 21:06:30 2022) The ideal is perhaps having it prefer the outer borders, but that's a lot of work for a dubious use case
			// Do nothing special for now, just ignore the edge during placement and restore it at the end to avoid panics
			// Without this, both the inner node and container would consider each other for median placements
			// ┌──────────┐
			// │     ▲    │
			// │     │    │
			// │   ┌─┼─┐  │
			// │   └───┘  │
			// │          │
			// └──────────┘
			containerAbductions = append(containerAbductions, &EdgeAbduction{
				Edge:           edge,
				OriginallyTo:   edge.To,
				OriginallyFrom: edge.From,
			})
			edge.From.removeEdge(edge)
			edge.To.removeEdge(edge)
		} else if fromAChild && toAChild {
			toGraph.AddEdge(edge)
			for i, ea := range edgeAbductions {
				if used[i] {
					continue
				}
				if ea.CurrentFrom == edge.From && ea.CurrentTo == edge.To {
					containerAbduction := &EdgeAbduction{
						Edge:        edge,
						CurrentFrom: edge.From,
						CurrentTo:   edge.To,
					}
					if ea.OriginallyFrom != nil {
						containerAbduction.OriginallyFrom = ea.OriginallyFrom
					} else {
						containerAbduction.OriginallyTo = ea.OriginallyTo
					}
					used[i] = true
					containerAbductions = append(containerAbductions, containerAbduction)
					break
				}
			}
		} else {
			// Else, if the edge is connected at one end to a child and another to a nested child, then temporarily reassign
			if fromAChildDescendant && toAChild {
				containerAbductions = append(containerAbductions, &EdgeAbduction{
					Edge:           edge,
					OriginallyFrom: edge.From,
					CurrentFrom:    fromChildAncestor,
					CurrentTo:      edge.To,
				})
				edge.reconnect(fromChildAncestor, false)
				toGraph.AddEdge(edge)
			} else if toAChildDescendant && fromAChild {
				containerAbductions = append(containerAbductions, &EdgeAbduction{
					Edge:         edge,
					OriginallyTo: edge.To,
					CurrentTo:    toChildAncestor,
					CurrentFrom:  edge.From,
				})
				edge.reconnect(toChildAncestor, true)
				toGraph.AddEdge(edge)
			} else if fromAChildDescendant && toAChildDescendant && (fromChildAncestor != toChildAncestor) {
				// Edge can also be from one child descendant to another
				// But if they are the same ancestor, then leave it be
				containerAbductions = append(containerAbductions, &EdgeAbduction{
					Edge:           edge,
					OriginallyTo:   edge.To,
					OriginallyFrom: edge.From,
					CurrentTo:      toChildAncestor,
					CurrentFrom:    fromChildAncestor,
				})
				edge.reconnect(fromChildAncestor, false)
				edge.reconnect(toChildAncestor, true)
				toGraph.AddEdge(edge)
			}
		}
	}

	// For every edge abduction, the current or original pointer at to or from can be a vessel
	for _, containerAbduction := range containerAbductions {
		for i, ea := range edgeAbductions {
			if used[i] {
				continue
			}
			match := false

			if containerAbduction.OriginallyFrom == nil { // Must have abducted To
				if containerAbduction.CurrentFrom == ea.CurrentFrom && containerAbduction.OriginallyTo == ea.CurrentTo {
					match = true
				}
			} else if containerAbduction.OriginallyTo == nil { // Must have abducted From
				if containerAbduction.CurrentTo == ea.CurrentTo && containerAbduction.OriginallyFrom == ea.CurrentFrom {
					match = true
				}
			} else { // Must have abducted both
				if containerAbduction.OriginallyTo == ea.CurrentTo && containerAbduction.OriginallyFrom == ea.CurrentFrom {
					match = true
				}
			}
			if match {
				used[i] = true
				if ea.OriginallyFrom != nil {
					containerAbduction.OriginallyFrom = ea.OriginallyFrom
				} else {
					containerAbduction.OriginallyTo = ea.OriginallyTo
				}
				break
			}
		}
	}

	return containerAbductions
}

// isDescendantOf walks up from maybeDescendant through containers, cluster
// vessels and sequence vessels. It is the iterative form of
// isDescendantOfRecursive, which it falls back to after an implausibly deep
// walk so that malformed (cyclic) ownership fails exactly as before.
func (maybeDescendant *Node) isDescendantOf(maybeAncestor *Node) bool {
	const iterativeDepth = 1 << 16
	for range iterativeDepth {
		if maybeAncestor == maybeDescendant {
			return true
		}
		if maybeDescendant == nil {
			return false
		}
		switch {
		case maybeDescendant.Container != nil:
			maybeDescendant = maybeDescendant.Container
		case maybeDescendant.Cluster != nil:
			maybeDescendant = maybeDescendant.Cluster.Vessel
		case maybeDescendant.Sequence != nil:
			maybeDescendant = maybeDescendant.Sequence.Vessel
		default:
			// Covers the case of nil ancestor
			return maybeAncestor == nil
		}
	}
	return maybeDescendant.isDescendantOfRecursive(maybeAncestor)
}

// ancestryWalkDepth bounds the iterative ancestry walks; deeper (cyclic)
// ownership falls back to the recursive walk.
const ancestryWalkDepth = 1 << 16

// ancestryNext returns the node isDescendantOf visits after node, and false
// when node has no container, cluster or sequence owner.
func ancestryNext(node *Node) (*Node, bool) {
	switch {
	case node.Container != nil:
		return node.Container, true
	case node.Cluster != nil:
		return node.Cluster.Vessel, true
	case node.Sequence != nil:
		return node.Sequence.Vessel, true
	default:
		return nil, false
	}
}

// IsDescendantOfAny reports whether node.IsDescendantOf(a) holds for any a in
// ancestors, evaluated in order like a chain of ||, with one walk up from node
// instead of one per ancestor. isDescendantOf(a) is true exactly when a is nil
// or the walk visits a, so one complete walk answers every ancestor; the first
// ancestor may answer early, exactly as the first || operand does. A walk that
// has not ended after ancestryWalkDepth steps (cyclic ownership), or more than
// 64 ancestors, evaluates the original expression, which fails as before.
func (node *Node) IsDescendantOfAny(ancestors ...*Node) bool {
	if len(ancestors) == 0 {
		return false
	}
	if len(ancestors) <= 64 {
		var found uint64
		for i, ancestor := range ancestors {
			if ancestor == nil {
				found |= 1 << i
			}
		}
		first := ancestors[0]
		current := node
		for range ancestryWalkDepth {
			if current == first {
				return true
			}
			for i, ancestor := range ancestors {
				if current == ancestor {
					found |= 1 << i
				}
			}
			if current == nil {
				return found != 0
			}
			next, ok := ancestryNext(current)
			if !ok {
				return found != 0
			}
			current = next
		}
	}
	for _, ancestor := range ancestors {
		if node.isDescendantOf(ancestor) {
			return true
		}
	}
	return false
}

// AppendAncestry appends node and every owner isDescendantOf visits above it
// (nearest first) to chain. For a non-nil ancestor a, node.IsDescendantOf(a)
// is then exactly whether a is in the appended nodes, and IsDescendantOf(nil)
// is always true. ok is false when the walk has not ended after
// ancestryWalkDepth steps (cyclic ownership); callers must then use
// IsDescendantOf, which fails as before.
func (node *Node) AppendAncestry(chain []*Node) ([]*Node, bool) {
	current := node
	for range ancestryWalkDepth {
		if current == nil {
			return chain, true
		}
		chain = append(chain, current)
		next, ok := ancestryNext(current)
		if !ok {
			return chain, true
		}
		current = next
	}
	return chain, false
}

func (maybeDescendant *Node) isDescendantOfRecursive(maybeAncestor *Node) bool {
	if maybeAncestor == maybeDescendant {
		return true
	}
	if maybeDescendant == nil {
		return false
	}
	if maybeDescendant.Container != nil {
		return maybeDescendant.Container.isDescendantOfRecursive(maybeAncestor)
	}
	if maybeDescendant.Cluster != nil {
		return maybeDescendant.Cluster.Vessel.isDescendantOfRecursive(maybeAncestor)
	}
	if maybeDescendant.Sequence != nil {
		return maybeDescendant.Sequence.Vessel.isDescendantOfRecursive(maybeAncestor)
	}
	// Covers the case of nil ancestor
	return maybeAncestor == nil
}

// routes is provided when graph is still undergoing edge routing and the edges don't have points locked in yet.
func (g *Graph) ComputeNodeSpacing() {
	for _, node := range g.Nodes {
		node.UpdateSpacing()
	}
}
