package routing

import "sort"

// Keeps OVG edges as one set for vertical edges and other set of horizontal ones.
// Can quickly check for intersections/overlaps
//
// Each axis keeps its distinct line coordinates sorted, with the edges on a
// line stored at the same index. Range scans therefore walk the per-line edge
// lists directly instead of hashing every visited coordinate.
type ovgEdgeSet struct {
	// horizontalLists[i] holds the edges on y = horizontals[i], ordered by
	// minimum X; verticalLists[i] holds the edges on x = verticals[i], ordered
	// by minimum Y.
	horizontals     []float64
	horizontalLists [][]OVGEdge
	verticals       []float64
	verticalLists   [][]OVGEdge
	edges           map[OVGEdge]struct{}
}

func newOvgEdgeSet() *ovgEdgeSet {
	return &ovgEdgeSet{
		horizontals:     make([]float64, 0, 16),
		horizontalLists: make([][]OVGEdge, 0, 16),
		verticals:       make([]float64, 0, 16),
		verticalLists:   make([][]OVGEdge, 0, 16),
		edges:           make(map[OVGEdge]struct{}, 16),
	}
}

// lineIndex returns the index of coordinate in the sorted line coordinates and
// whether that line exists. It matches float64 map-key equality for the finite
// coordinates routing uses (including -0 == +0).
func lineIndex(coordinates []float64, coordinate float64) (int, bool) {
	i := sort.SearchFloat64s(coordinates, coordinate)
	return i, i < len(coordinates) && coordinates[i] == coordinate
}

// horizontalEdgesAt returns the edges on the horizontal line y.
func (set *ovgEdgeSet) horizontalEdgesAt(y float64) []OVGEdge {
	if i, ok := lineIndex(set.horizontals, y); ok {
		return set.horizontalLists[i]
	}
	return nil
}

// verticalEdgesAt returns the edges on the vertical line x.
func (set *ovgEdgeSet) verticalEdgesAt(x float64) []OVGEdge {
	if i, ok := lineIndex(set.verticals, x); ok {
		return set.verticalLists[i]
	}
	return nil
}

// lineSlot returns the index of coordinate's edge list, inserting an empty
// line in sorted position when it does not exist yet.
func lineSlot(coordinates *[]float64, lists *[][]OVGEdge, coordinate float64) int {
	i, ok := lineIndex(*coordinates, coordinate)
	if ok {
		return i
	}
	*coordinates = append(*coordinates, 0)
	copy((*coordinates)[i+1:], (*coordinates)[i:])
	(*coordinates)[i] = coordinate
	*lists = append(*lists, nil)
	copy((*lists)[i+1:], (*lists)[i:])
	(*lists)[i] = make([]OVGEdge, 0, 16)
	return i
}

// insertSortedEdge inserts an edge into a sorted edge slice while maintaining order
// Edges are sorted by their minimum coordinate (Y for vertical, X for horizontal)
func (set *ovgEdgeSet) insertSortedEdge(edges *[]OVGEdge, edge OVGEdge, isVertical bool) {
	getMinCoord := func(e OVGEdge) float64 {
		if isVertical {
			if e.From.Y < e.To.Y {
				return e.From.Y
			}
			return e.To.Y
		} else {
			if e.From.X < e.To.X {
				return e.From.X
			}
			return e.To.X
		}
	}

	edgeMinCoord := getMinCoord(edge)

	// Binary search for insertion point
	i := sort.Search(len(*edges), func(j int) bool {
		return getMinCoord((*edges)[j]) >= edgeMinCoord
	})

	*edges = append(*edges, OVGEdge{})
	copy((*edges)[i+1:], (*edges)[i:])
	(*edges)[i] = edge
}

func (set *ovgEdgeSet) add(edge *OVGEdge) {
	// The set stores geometry only; an OVG edge's creation index is not part of
	// its identity here.
	key := OVGEdge{From: edge.From, To: edge.To, Distance: edge.Distance}
	if _, exists := set.edges[key]; exists {
		return
	}
	set.edges[key] = struct{}{}

	if edge.isVertical() {
		i := lineSlot(&set.verticals, &set.verticalLists, edge.From.X)
		set.insertSortedEdge(&set.verticalLists[i], key, true)
	} else if edge.isHorizontal() {
		i := lineSlot(&set.horizontals, &set.horizontalLists, edge.From.Y)
		set.insertSortedEdge(&set.horizontalLists[i], key, false)
	}
}

// addGuarded reserves every sorted-slice shift before mutating the set. The
// duplicate lookup itself is also accounted, and a guard failure leaves the set
// unchanged.
func (set *ovgEdgeSet) addGuarded(edge *OVGEdge, guard workBudget) error {
	if err := guard.step(); err != nil {
		return err
	}
	if _, exists := set.edges[OVGEdge{From: edge.From, To: edge.To, Distance: edge.Distance}]; exists {
		return nil
	}
	if edge.isVertical() {
		i, exists := lineIndex(set.verticals, edge.From.X)
		if !exists {
			if err := guard.add(uint64(len(set.verticals)) + 1); err != nil {
				return err
			}
		}
		lineLength := 0
		if exists {
			lineLength = len(set.verticalLists[i])
		}
		if err := guard.add(uint64(lineLength) + 1); err != nil {
			return err
		}
	} else if edge.isHorizontal() {
		i, exists := lineIndex(set.horizontals, edge.From.Y)
		if !exists {
			if err := guard.add(uint64(len(set.horizontals)) + 1); err != nil {
				return err
			}
		}
		lineLength := 0
		if exists {
			lineLength = len(set.horizontalLists[i])
		}
		if err := guard.add(uint64(lineLength) + 1); err != nil {
			return err
		}
	}
	set.add(edge)
	return guard.check()
}

func (set *ovgEdgeSet) intersectsWithGuarded(edge *OVGEdge, guard workBudget) (bool, error) {
	return set.intersectsWithChecked(edge, guard)
}

func (set *ovgEdgeSet) intersectsWithChecked(edge *OVGEdge, guard workBudget) (bool, error) {
	step := func() error {
		if guard == nil {
			return nil
		}
		return guard.step()
	}
	if edge.isHorizontal() {
		minX, maxX := edge.From.X, edge.To.X
		if maxX < minX {
			minX, maxX = maxX, minX
		}

		// check for overlaps
		for _, e := range set.horizontalEdgesAt(edge.From.Y) {
			if err := step(); err != nil {
				return false, err
			}
			if e.From.X < e.To.X {
				if maxX <= e.From.X {
					// horizontalEdges is ordered by minX, so no following edges have a minX less than this one
					break
				}
				if e.To.X <= minX {
					continue
				}
			} else {
				if maxX <= e.To.X {
					break
				}
				if e.From.X <= minX {
					continue
				}
			}
			if !edge.sharePoints(e) {
				return true, nil
			}
		}

		y := edge.From.Y
		// The axes are sorted. Charge the skipped prefix as before, but begin
		// geometry checks at the first line that can cross this segment.
		start := sort.Search(len(set.verticals), func(i int) bool { return !(set.verticals[i] < minX) })
		if err := chargeSkippedRouteWork(guard, start); err != nil {
			return false, err
		}
		// check for crossings
		for lineOffset, lineCoordinate := range set.verticals[start:] {
			if err := step(); err != nil {
				return false, err
			}
			// when edge is horizontal, lineCoordinate is the X of vertical lines
			// so we filter out the vertical lines outside the span of edge as they won't intersect
			// similarly, it happens when edge is vertical and we filter the horizontal ones
			if lineCoordinate < minX {
				continue
			}
			if maxX < lineCoordinate {
				break
			}
			for _, e := range set.verticalLists[start+lineOffset] {
				if err := step(); err != nil {
					return false, err
				}
				if e.From.Y < e.To.Y {
					if y < e.From.Y {
						break
					}
					if e.To.Y < y {
						continue
					}
				} else {
					if y < e.To.Y {
						break
					}
					if e.From.Y < y {
						continue
					}
				}
				if !edge.sharePoints(e) {
					return true, nil
				}
			}
		}
	} else if edge.isVertical() {
		minY, maxY := edge.From.Y, edge.To.Y
		if maxY < minY {
			minY, maxY = maxY, minY
		}
		// check for overlaps
		for _, e := range set.verticalEdgesAt(edge.From.X) {
			if err := step(); err != nil {
				return false, err
			}
			if e.From.Y < e.To.Y {
				if maxY <= e.From.Y {
					// verticalEdges is ordered by minY, so no following edges have a minY less than this one
					break
				}
				if e.To.Y <= minY {
					continue
				}
			} else {
				if maxY <= e.To.Y {
					break
				}
				if e.From.Y <= minY {
					continue
				}
			}
			if !edge.sharePoints(e) {
				return true, nil
			}
		}

		x := edge.From.X
		start := sort.Search(len(set.horizontals), func(i int) bool { return !(set.horizontals[i] < minY) })
		if err := chargeSkippedRouteWork(guard, start); err != nil {
			return false, err
		}
		for lineOffset, lineCoordinate := range set.horizontals[start:] {
			if err := step(); err != nil {
				return false, err
			}
			if lineCoordinate < minY {
				continue
			}
			if maxY < lineCoordinate {
				break
			}
			for _, e := range set.horizontalLists[start+lineOffset] {
				if err := step(); err != nil {
					return false, err
				}
				if e.From.X < e.To.X {
					if x < e.From.X {
						break
					}
					if e.To.X < x {
						continue
					}
				} else {
					if x < e.To.X {
						break
					}
					if e.From.X < x {
						continue
					}
				}
				if !edge.sharePoints(e) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (set *ovgEdgeSet) overlappingEdgesGuarded(edge *OVGEdge, guard workBudget) ([]OVGEdge, error) {
	return set.overlappingEdgesChecked(edge, guard)
}

func (set *ovgEdgeSet) overlappingEdgesChecked(edge *OVGEdge, guard workBudget) ([]OVGEdge, error) {
	var overlapping []OVGEdge
	var maybeOverlap []OVGEdge
	if edge.isVertical() {
		maybeOverlap = set.verticalEdgesAt(edge.From.X)
	} else if edge.isHorizontal() {
		maybeOverlap = set.horizontalEdgesAt(edge.From.Y)
	}

	for _, e := range maybeOverlap {
		if guard != nil {
			if err := guard.step(); err != nil {
				return nil, err
			}
		}
		if intersects(edge.From.Point, edge.To.Point, e.From.Point, e.To.Point) {
			overlapping = append(overlapping, e)
		}
	}

	return overlapping, nil
}
