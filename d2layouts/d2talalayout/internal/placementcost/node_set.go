package placementcost

import "github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"

// nodeSetLinearLimit is the size up to which nodeSet answers membership by
// scanning its members. Scoring neighborhoods are usually a handful of nodes,
// where a scan is cheaper than hashing and, unlike a pooled map, resetting
// costs nothing beyond the members themselves.
const nodeSetLinearLimit = 32

// nodeSet is a membership set of nodes that scans while small and indexes
// itself with a map once it grows past nodeSetLinearLimit. It is a drop-in
// replacement for a map[*layoutgraph.Node]struct{} used only for membership.
type nodeSet struct {
	members []*layoutgraph.Node
	index   map[*layoutgraph.Node]struct{}
}

func (set *nodeSet) has(node *layoutgraph.Node) bool {
	if set.index != nil {
		_, ok := set.index[node]
		return ok
	}
	for _, member := range set.members {
		if member == node {
			return true
		}
	}
	return false
}

// add inserts node; adding an existing member has no effect.
func (set *nodeSet) add(node *layoutgraph.Node) {
	if set.has(node) {
		return
	}
	set.members = append(set.members, node)
	if set.index != nil {
		set.index[node] = struct{}{}
		return
	}
	if len(set.members) > nodeSetLinearLimit {
		set.index = make(map[*layoutgraph.Node]struct{}, 2*len(set.members))
		for _, member := range set.members {
			set.index[member] = struct{}{}
		}
	}
}

// reset empties the set and releases any map index so pooled sets never pay
// for clearing a large map.
func (set *nodeSet) reset() {
	clear(set.members)
	if cap(set.members) > maxPooledEdgeScratchEntries {
		set.members = nil
	} else {
		set.members = set.members[:0]
	}
	set.index = nil
}
