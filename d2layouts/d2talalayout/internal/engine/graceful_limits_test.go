package engine

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/log"
)

func gracefulLimitsContext(t *testing.T) context.Context {
	return log.Leveled(withTestLogger(t.Context(), t), slog.LevelError)
}

// layoutWithWorkLimits runs one complete layout through the production entry
// point with limits installed the same way the adapter's tests install them,
// and checks the structural layout invariants.
func layoutWithWorkLimits(t *testing.T, fixture string, limits WorkLimits) (*pipeline, *layoutgraph.Graph) {
	t.Helper()
	ctx := ContextWithWorkLimits(gracefulLimitsContext(t), limits)
	path := filepath.Join("..", "testdata", fixture)
	graph, err := readGraph(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := runLayout(ctx, graph, LayoutOptions{Seed: 7}, pipelineInstrumentation{}, true)
	if err != nil {
		t.Fatalf("%s with limits %+v: %v", fixture, limits, err)
	}
	start, err := readGraph(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkGraphPropertiesAfterAutolayout(start, graph); err != nil {
		t.Fatalf("%s with limits %+v: %v", fixture, limits, err)
	}
	for _, edge := range graph.Edges {
		if !hasCompleteEdgeRoute(edge) {
			t.Fatalf("%s with limits %+v: edge %s has no route", fixture, limits, edge.DebugID())
		}
	}
	return p, graph
}

func withGOMAXPROCS(procs int, run func()) {
	previous := runtime.GOMAXPROCS(procs)
	defer runtime.GOMAXPROCS(previous)
	run()
}

// Exhausted budgets degrade every fixture to a complete, valid layout whose
// bytes are independent of the run and of how route flavors are scheduled.
func TestExhaustedWorkLimitsDegradeDeterministically(t *testing.T) {
	cases := []struct {
		name   string
		limits WorkLimits
		check  func(*testing.T, *pipeline) bool
	}{
		{
			name:   "refinement",
			limits: WorkLimits{RefinementTotal: 400, RefinementStage: 200},
			check: func(t *testing.T, p *pipeline) bool {
				return p.skippedRefinementStages > 0 || p.refinement.Used() >= 200
			},
		},
		{
			name:   "route search",
			limits: WorkLimits{RouteSearch: 1},
			check: func(t *testing.T, p *pipeline) bool {
				return p.routeDiagnostics.FallbackRoutes > 0
			},
		},
		{
			name:   "visibility graph",
			limits: WorkLimits{OVGCandidates: 1},
			check: func(t *testing.T, p *pipeline) bool {
				return p.routeDiagnostics.PortGraphs > 0
			},
		},
		{
			name:   "all",
			limits: WorkLimits{RefinementTotal: 1, RefinementStage: 1, RouteSearch: 1, OVGCandidates: 1},
			check: func(t *testing.T, p *pipeline) bool {
				return p.skippedRefinementStages > 0 && p.routeDiagnostics.FallbackRoutes > 0 && p.routeDiagnostics.PortGraphs > 0
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			degraded := 0
			for _, fixture := range gracefulLimitFixtures {
				var first, second *layoutgraph.Graph
				var p *pipeline
				withGOMAXPROCS(1, func() { p, first = layoutWithWorkLimits(t, fixture, test.limits) })
				withGOMAXPROCS(4, func() { _, second = layoutWithWorkLimits(t, fixture, test.limits) })
				requireGraphsSerializeEqual(t.Context(), t, first, second)
				if test.check(t, p) {
					degraded++
				}
			}
			if degraded == 0 {
				t.Fatalf("no fixture reached the %s limit", test.name)
			}
		})
	}
}

// Budgets that are never used up have no effect: the meters only observe.
func TestUnexhaustedWorkLimitsLeaveLayoutUnchanged(t *testing.T) {
	for _, fixture := range gracefulLimitFixtures[:min(4, len(gracefulLimitFixtures))] {
		p, defaults := layoutWithWorkLimits(t, fixture, WorkLimits{})
		if p.skippedRefinementStages != 0 || p.routeDiagnostics.FallbackRoutes != 0 || p.routeDiagnostics.PortGraphs != 0 {
			t.Fatalf("%s degraded under production limits: %d skipped stages, %+v", fixture, p.skippedRefinementStages, p.routeDiagnostics)
		}
		_, unlimited := layoutWithWorkLimits(t, fixture, WorkLimits{
			RefinementTotal: math.MaxInt64,
			RefinementStage: math.MaxInt64,
			RouteSearch:     math.MaxUint64,
		})
		requireGraphsSerializeEqual(t.Context(), t, defaults, unlimited)
	}
}

// Cancellation stays an error when a budget is exhausted at the same time.
func TestCancellationWinsOverDegradation(t *testing.T) {
	ctx, cancel := context.WithCancel(ContextWithWorkLimits(gracefulLimitsContext(t), WorkLimits{RefinementTotal: 1, RefinementStage: 1, RouteSearch: 1}))
	cancel()
	graph, err := readGraph(t.Context(), filepath.Join("..", "testdata", gracefulLimitFixtures[0]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Layout(ctx, graph, LayoutOptions{Seed: 7}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Layout error = %v, want context.Canceled", err)
	}
}

// FuzzAutolayoutDegraded lays out random graphs with exhausted work budgets:
// every attempt must complete with valid, deterministic output.
func FuzzAutolayoutDegraded(f *testing.F) {
	f.Add(int64(1), uint8(10), uint8(2), uint8(50), uint8(40), uint8(10), uint8(40), uint8(0), uint16(1))
	f.Add(int64(2), uint8(15), uint8(4), uint8(60), uint8(60), uint8(25), uint8(65), uint8(1), uint16(300))
	f.Add(int64(3), uint8(20), uint8(5), uint8(40), uint8(80), uint8(45), uint8(80), uint8(2), uint16(7))
	f.Add(int64(4), uint8(25), uint8(3), uint8(30), uint8(70), uint8(30), uint8(70), uint8(3), uint16(2000))

	f.Fuzz(func(
		t *testing.T,
		seed int64,
		maxNodes uint8,
		connectionIterations uint8,
		compactionPercent uint8,
		nodeSubsetPercent uint8,
		minConnectionPercent uint8,
		maxConnectionPercent uint8,
		limitMode uint8,
		limit uint16,
	) {
		config := normalizeFuzzerConfig(seed, maxNodes, connectionIterations, compactionPercent, nodeSubsetPercent, minConnectionPercent, maxConnectionPercent)
		units := int64(limit) + 1
		var limits WorkLimits
		switch limitMode % 4 {
		case 0:
			limits = WorkLimits{RefinementTotal: units, RefinementStage: units/2 + 1}
		case 1:
			limits = WorkLimits{RouteSearch: uint64(units) * 10}
		case 2:
			limits = WorkLimits{OVGCandidates: uint64(units)}
		default:
			limits = WorkLimits{RefinementTotal: units, RefinementStage: units, RouteSearch: uint64(units), OVGCandidates: uint64(units)}
		}

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		ctx = ContextWithWorkLimits(log.Leveled(withTestLogger(ctx, t), slog.LevelError), limits)

		layout := func() (*layoutgraph.Graph, *layoutgraph.Graph) {
			graph := createRandomGraph(ctx, config)
			before := roundTripGraph(ctx, t, graph)
			if _, err := layoutWithSnapshots(ctx, graph, config.seed, false); err != nil {
				t.Fatalf("autolayout: %v; config: %+v; limits: %+v", err, config, limits)
			}
			return before, graph
		}
		before, graph := layout()
		if err := checkStructuralGraphPropertiesAfterAutolayout(before, graph); err != nil {
			t.Fatalf("layout invariants: %v; config: %+v; limits: %+v", err, config, limits)
		}
		if err := checkOverlap(graph); err != nil {
			t.Fatalf("layout overlap: %v; config: %+v; limits: %+v", err, config, limits)
		}
		_, again := layout()
		requireGraphsSerializeEqual(ctx, t, graph, again)
	})
}
