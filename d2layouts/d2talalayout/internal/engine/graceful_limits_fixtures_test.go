//go:build !race

package engine

// Fixtures with containers, clusters, sequences, trees, hierarchies, tables,
// self edges and several disconnected subgraphs, so every degradation path
// meets each layout structure.
var gracefulLimitFixtures = []string{
	"layout/n22_e32/graph.input.json",
	"layout/large_nested/graph.input.json",
	"layout/clusters/graph.input.json",
	"layout/sequence/graph.input.json",
	"layout/larger_tree/graph.input.json",
	"layout/hierarchy_inside_containers/graph.input.json",
	"layout/hierarchy_subgraphs/graph.input.json",
	"layout/self_edges_all_shapes/graph.input.json",
	"layout/container_escape/graph.input.json",
	"benchmark/18tables.json",
}
