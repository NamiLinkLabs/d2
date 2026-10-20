package routing

import (
	"math"
	"slices"
	"sort"
	"sync"

	"github.com/d2lang/d2/lib/geo"
)

// ovgPointIndex assigns every distinct OVG node coordinate a dense ID so the
// router's per-point state can live in slices instead of coordinate-keyed
// maps. IDs depend only on coordinate values: two nodes at the same point
// share an ID, exactly like a map[geo.Point] key would.
//
// The index is built once per OVG, after construction, and is read-only while
// flavor routers run concurrently.
type ovgPointIndex struct {
	ids map[geo.Point]int32
	// nodeIDs and nodePoints are indexed by OVGNode.Index. nodePoints records
	// the coordinate each cached ID was derived from so a node whose point
	// changed after indexing falls back to the coordinate map.
	nodeIDs    []int32
	nodePoints []geo.Point
}

type ovgPointIndexCache struct {
	mu    sync.Mutex
	index *ovgPointIndex
	nodes int
}

// pointIndex returns the OVG's point index, rebuilding it when nodes were
// added since it was last built.
func (g *OVG) pointIndex() *ovgPointIndex {
	g.pointIndexCache.mu.Lock()
	defer g.pointIndexCache.mu.Unlock()
	if g.pointIndexCache.index != nil && g.pointIndexCache.nodes == len(g.Nodes) {
		return g.pointIndexCache.index
	}
	index := &ovgPointIndex{
		ids:        make(map[geo.Point]int32, len(g.Nodes)),
		nodeIDs:    make([]int32, len(g.Nodes)),
		nodePoints: make([]geo.Point, len(g.Nodes)),
	}
	for i, node := range g.Nodes {
		index.nodeIDs[i] = -1
		if node == nil || node.Point == nil || node.Index != i {
			continue
		}
		point := *node.Point
		id, ok := index.ids[point]
		if !ok {
			id = int32(len(index.ids))
			index.ids[point] = id
		}
		index.nodeIDs[i] = id
		index.nodePoints[i] = point
	}
	g.pointIndexCache.index = index
	g.pointIndexCache.nodes = len(g.Nodes)
	return index
}

// routePointTable is a router's point -> routes table. It is equivalent to a
// map[geo.Point][]*Route keyed by OVG node coordinates.
type routePointTable struct {
	ovg     *OVG
	index   *ovgPointIndex
	routes  [][]*Route
	foreign map[geo.Point]int32
}

func newRoutePointTable(ovg *OVG) *routePointTable {
	table := &routePointTable{ovg: ovg}
	if ovg != nil {
		table.index = ovg.pointIndex()
		table.routes = make([][]*Route, len(table.index.ids))
	}
	return table
}

// id returns the dense ID of node's coordinate. With allocate, a coordinate
// that is not an OVG node point receives a new router-local ID.
func (table *routePointTable) id(node *OVGNode, allocate bool) (int32, bool) {
	point := *node.Point
	if index := table.index; index != nil {
		if i := node.Index; uint(i) < uint(len(index.nodeIDs)) && i < len(table.ovg.Nodes) && table.ovg.Nodes[i] == node &&
			index.nodeIDs[i] >= 0 && index.nodePoints[i] == point {
			return index.nodeIDs[i], true
		}
		if id, ok := index.ids[point]; ok {
			return id, true
		}
	}
	if id, ok := table.foreign[point]; ok {
		return id, true
	}
	if !allocate {
		return 0, false
	}
	if table.foreign == nil {
		table.foreign = make(map[geo.Point]int32)
	}
	id := int32(len(table.routes))
	table.foreign[point] = id
	table.routes = append(table.routes, nil)
	return id, true
}

// at returns the routes through node's coordinate.
func (table *routePointTable) at(node *OVGNode) []*Route {
	if id, ok := table.id(node, false); ok {
		return table.routes[id]
	}
	return nil
}

// add records route as passing through node's coordinate.
func (table *routePointTable) add(node *OVGNode, route *Route) {
	id, _ := table.id(node, true)
	table.routes[id] = append(table.routes[id], route)
}

// len reports the number of coordinates with at least one route.
func (table *routePointTable) len() int {
	count := 0
	for _, routes := range table.routes {
		if len(routes) > 0 {
			count++
		}
	}
	return count
}

// routeEdgeTable keeps the router's per-OVG-edge nearby and overlap state.
// Edges created by the router's OVG are stored densely by creation index;
// any other edge falls back to pointer-keyed maps.
type routeEdgeTable struct {
	ovg             *OVG
	nearby          []bool
	overlaps        [][]*Route
	foreignNearby   map[*OVGEdge]struct{}
	foreignOverlaps map[*OVGEdge][]*Route
}

func newRouteEdgeTable(ovg *OVG) *routeEdgeTable {
	table := &routeEdgeTable{ovg: ovg}
	if ovg != nil {
		table.nearby = make([]bool, len(ovg.Edges))
		table.overlaps = make([][]*Route, len(ovg.Edges))
	}
	return table
}

func (table *routeEdgeTable) slot(edge *OVGEdge) (int, bool) {
	i := edge.index
	if uint(i) < uint(len(table.nearby)) && i < len(table.ovg.Edges) && table.ovg.Edges[i] == edge {
		return i, true
	}
	return 0, false
}

func (table *routeEdgeTable) hasNearby(edge *OVGEdge) bool {
	if i, ok := table.slot(edge); ok {
		return table.nearby[i]
	}
	_, ok := table.foreignNearby[edge]
	return ok
}

func (table *routeEdgeTable) markNearby(edge *OVGEdge) {
	if i, ok := table.slot(edge); ok {
		table.nearby[i] = true
		return
	}
	if table.foreignNearby == nil {
		table.foreignNearby = make(map[*OVGEdge]struct{})
	}
	table.foreignNearby[edge] = struct{}{}
}

func (table *routeEdgeTable) overlapping(edge *OVGEdge) []*Route {
	if i, ok := table.slot(edge); ok {
		return table.overlaps[i]
	}
	return table.foreignOverlaps[edge]
}

func (table *routeEdgeTable) addOverlapping(edge *OVGEdge, route *Route) {
	if i, ok := table.slot(edge); ok {
		table.overlaps[i] = append(table.overlaps[i], route)
		return
	}
	if table.foreignOverlaps == nil {
		table.foreignOverlaps = make(map[*OVGEdge][]*Route)
	}
	table.foreignOverlaps[edge] = append(table.foreignOverlaps[edge], route)
}

// ovgAxisIndex lists the keys of OVG.VerticalEdges and OVG.HorizontalEdges
// in ascending order with their edge lists, so a router can visit the lines
// near a coordinate without iterating the whole map. It is built once per OVG
// and read-only while flavor routers run.
type ovgAxisIndex struct {
	verticalXs      []float64
	verticalEdges   [][]*OVGEdge
	horizontalYs    []float64
	horizontalEdges [][]*OVGEdge
	// ordered is false when a key is NaN; such keys have no position in the
	// order, so callers must iterate the maps instead.
	ordered bool
}

type ovgAxisIndexCache struct {
	mu    sync.Mutex
	index *ovgAxisIndex
	edges int
}

func (g *OVG) axisIndex() *ovgAxisIndex {
	g.axisIndexCache.mu.Lock()
	defer g.axisIndexCache.mu.Unlock()
	if g.axisIndexCache.index != nil && g.axisIndexCache.edges == len(g.Edges) {
		return g.axisIndexCache.index
	}
	index := &ovgAxisIndex{ordered: true}
	collect := func(lines map[float64][]*OVGEdge) ([]float64, [][]*OVGEdge) {
		keys := make([]float64, 0, len(lines))
		for key := range lines {
			if math.IsNaN(key) {
				index.ordered = false
				continue
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		edges := make([][]*OVGEdge, len(keys))
		for i, key := range keys {
			edges[i] = lines[key]
		}
		return keys, edges
	}
	index.verticalXs, index.verticalEdges = collect(g.VerticalEdges)
	index.horizontalYs, index.horizontalEdges = collect(g.HorizontalEdges)
	g.axisIndexCache.index = index
	g.axisIndexCache.edges = len(g.Edges)
	return index
}

// nearbyLines returns the index range of keys that may lie within distance of
// coordinate. keys must be sorted and coordinate finite; callers still apply
// their exact distance predicate to every key in the range. The range bounds
// use a margin of more than twice the distance: rounding is monotonic, so
// every key whose computed distance is at most distance lies inside it.
func nearbyLines(keys []float64, coordinate, distance float64) (int, int) {
	margin := 2*distance + 1
	low := sort.SearchFloat64s(keys, coordinate-margin)
	high := low
	upper := coordinate + margin
	for high < len(keys) && keys[high] <= upper {
		high++
	}
	return low, high
}
