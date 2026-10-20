//go:build race

package engine

// The race detector slows layout down by more than an order of magnitude, so
// race runs cover the degradation paths with the smallest fixtures only.
var gracefulLimitFixtures = []string{
	"layout/clusters/graph.input.json",
	"layout/sequence/graph.input.json",
	"layout/container_escape/graph.input.json",
}
