//go:build !race

package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"log/slog"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/log"
)

// reducedTestEffort matches the per-seed pipeline of the adapter's balanced
// and fast quality levels.
var reducedTestEffort = Effort{
	PlacementIterations:  30,
	SingleRouteFlavor:    true,
	SkipSwap:             true,
	SkipGapNormalization: true,
}

// A reduced effort must stay a complete, valid and deterministic layout on
// every fixture the full pipeline handles.
func TestReducedEffortLayoutsFixtures(t *testing.T) {
	t.Parallel()
	names, err := testGraphNames()
	if err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]string, len(names))
	for _, name := range names {
		paths["layout/"+name] = filepath.Join(layoutTestDir, name, "graph.input.json")
	}
	regressions, err := filepath.Glob(filepath.Join("..", "testdata", "regression", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range regressions {
		paths["regression/"+strings.TrimSuffix(filepath.Base(path), ".json")] = path
	}
	if len(paths) < 40 {
		t.Fatalf("found only %d fixtures", len(paths))
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := log.Leveled(withTestLogger(t.Context(), t), slog.LevelError)
			var layouts [2]*layoutgraph.Graph
			for i := range layouts {
				graph, err := readGraph(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runLayout(ctx, graph, LayoutOptions{Seed: talaSeed, Effort: reducedTestEffort}, pipelineInstrumentation{}, true); err != nil {
					t.Fatalf("reduced-effort layout failed: %v", err)
				}
				layouts[i] = graph
			}
			start, err := readGraph(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkGraphPropertiesAfterAutolayout(start, layouts[0]); err != nil {
				// A few fixtures already violate these assertions with the full
				// pipeline; reduced effort must not add violations.
				full, readErr := readGraph(ctx, path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if _, layoutErr := runLayout(ctx, full, LayoutOptions{Seed: talaSeed}, pipelineInstrumentation{}, true); layoutErr != nil {
					t.Fatal(layoutErr)
				}
				fullErr := checkGraphPropertiesAfterAutolayout(start, full)
				if fullErr == nil || fullErr.Error() != err.Error() {
					t.Fatalf("reduced-effort layout is invalid: %v (full pipeline: %v)", err, fullErr)
				}
				t.Logf("same violation as the full pipeline: %v", err)
			}
			requireGraphsSerializeEqual(ctx, t, layouts[0], layouts[1])
		})
	}
}
