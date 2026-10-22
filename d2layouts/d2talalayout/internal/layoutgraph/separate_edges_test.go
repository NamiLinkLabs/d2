package layoutgraph_test

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/graphjson"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// SeparateEdges is a setting of the layout request: every copy of a graph
// that routes edges must keep it.
func TestSeparateEdgesSurvivesGraphCopies(t *testing.T) {
	source := newCloneParityGraph()
	if !source.SeparateEdges {
		t.Fatal("the parity fixture must set SeparateEdges")
	}
	cloned, err := layoutgraph.Clone(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if !cloned.SeparateEdges {
		t.Fatal("Clone dropped SeparateEdges")
	}

	serialized, err := graphjson.Serialize(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if !serialized.SeparateEdges {
		t.Fatal("graphjson.Serialize dropped SeparateEdges")
	}
	decoded := layoutgraph.NewGraph()
	if err := graphjson.Deserialize(t.Context(), serialized, decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.SeparateEdges {
		t.Fatal("graphjson.Deserialize dropped SeparateEdges")
	}

	// Routing splits a graph into subgraphs that share its entities.
	subgraph := layoutgraph.NewGraph()
	subgraph.CopyEntitiesFrom(source)
	if !subgraph.SeparateEdges {
		t.Fatal("CopyEntitiesFrom dropped SeparateEdges")
	}
}
