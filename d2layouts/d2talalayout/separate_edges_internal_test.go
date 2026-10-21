package d2talalayout

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestSeparateEdgesFromData(t *testing.T) {
	for _, tc := range []struct {
		raw  any
		want bool
	}{
		{"true", true},
		{"false", false},
		{true, true},
		{false, false},
	} {
		got, err := separateEdgesFromData(tc.raw)
		if err != nil || got != tc.want {
			t.Fatalf("separateEdgesFromData(%#v) = %v, %v; want %v", tc.raw, got, err, tc.want)
		}
	}
	for _, raw := range []any{"yes", "TRUE", "1", "", nil, 1, []any{"true"}} {
		if _, err := separateEdgesFromData(raw); err == nil || err.Error() != "tala-separate-edges must be true or false" {
			t.Fatalf("separateEdgesFromData(%#v) error = %v", raw, err)
		}
	}
}

func TestLayoutPlanSeparateEdgesPrecedence(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		opts *Options
		want bool
	}{
		{name: "nil options", want: false},
		{name: "zero options", opts: &Options{}, want: false},
		{name: "option", opts: &Options{SeparateEdges: true}, want: true},
		{name: "data", data: map[string]any{"tala-separate-edges": "true"}, want: true},
		{name: "data over option", data: map[string]any{"tala-separate-edges": "false"}, opts: &Options{SeparateEdges: true}, want: false},
		{name: "data with other keys", data: map[string]any{"tala-seeds": []any{"2"}, "tala-separate-edges": "true"}, opts: &Options{}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			graph := d2graph.NewGraph()
			graph.Data = tc.data
			request, err := layoutPlan(graph, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if request.separateEdges != tc.want {
				t.Fatalf("separateEdges = %v, want %v", request.separateEdges, tc.want)
			}
		})
	}

	graph := d2graph.NewGraph()
	graph.Data = map[string]any{"tala-separate-edges": "yes"}
	if _, err := layoutPlan(graph, &Options{SeparateEdges: true}); err == nil || !strings.Contains(err.Error(), "tala-separate-edges must be true or false") {
		t.Fatalf("invalid data error = %v", err)
	}
}

func TestValidateLayoutResultMetadataKeepsSeparateEdges(t *testing.T) {
	input := layoutgraph.NewGraph()
	input.SeparateEdges = true
	node := layoutgraph.NewNode(1, 140, 90)
	node.TopLeft = geo.NewPoint(0, 0)
	input.AddNewNodeToContainer(nil, node)

	completed, err := layoutgraph.Clone(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLayoutResultMetadata(t.Context(), input, completed, nil); err != nil {
		t.Fatalf("cloned result rejected: %v", err)
	}
	completed.SeparateEdges = false
	if err := validateLayoutResultMetadata(t.Context(), input, completed, nil); err == nil || !strings.Contains(err.Error(), "separate edges metadata changed") {
		t.Fatalf("result that lost SeparateEdges: error = %v", err)
	}
}
