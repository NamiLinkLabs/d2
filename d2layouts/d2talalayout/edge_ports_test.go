package d2talalayout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

// edgePortCases pin the last edge of each source with source-port / target-port.
var edgePortCases = []struct {
	name, source, src, dst string
}{
	{"facing", "a -> b", "right", "left"},
	{"same side", "a -> b", "top", "top"},
	{"adjacent sides", "a -> b", "left", "bottom"},
	{"reversed arrow", "a <- b", "bottom", "right"},
	{"source only", "a -> b", "left", ""},
	{"target only", "a -> b", "", "right"},
	{"triangle", "a -> b; b -> c; a -> c", "right", "top"},
	{"direction right", "direction: right; a -> b; b -> c; c -> d; a -> e; e -> d; a -> d", "bottom", "top"},
	{"tree", "a -> b -> c -> d; a -> f -> g; a -> g", "left", "right"},
	{"containers", "x: {a; b}; y: {c; d}; x.a -> y.c; x.b -> y.d", "top", "bottom"},
	{"container endpoints", "x: {a -> b}; y: {c}; x -> y", "right", "top"},
	{"circle to diamond", "a: {shape: circle}; b: {shape: diamond}; a -> b", "left", "right"},
	{"fan out", "a; b; c; d; a -> b; a -> c; b -> c; a -> d", "top", "left"},
	{"sql_table endpoint", "t: {shape: sql_table; id: int}; u; t.id -> u; t -> u", "bottom", "top"},
	{"diamond to side", "check: {shape: diamond}; save; check -> save: yes", "right", "top"},
	{"opposite pair", "check: {shape: diamond}; fix; check -> fix; fix -> check", "left", "left"},
	{"flowchart", "start: {shape: oval}; check: {shape: diamond}; save; fix; end: {shape: oval}; start -> check; check -> save: yes {source-port: right; target-port: top}; check -> fix: no {source-port: bottom; target-port: top}; save -> end; fix -> check", "left", "left"},
}

func pinEdge(t *testing.T, source, src, dst string) (*d2graph.Graph, *d2graph.Edge) {
	t.Helper()
	key := "(" + lastEdgeKey(t, source) + ")[0]"
	var pins []string
	if src != "" {
		pins = append(pins, key+".source-port: "+src)
	}
	if dst != "" {
		pins = append(pins, key+".target-port: "+dst)
	}
	g := compileD2ForPins(t, source+"\n"+strings.Join(pins, "\n"))
	return g, g.Edges[len(g.Edges)-1]
}

// lastEdgeKey returns the edge key of the last connection in source, which is
// written as "; "-separated statements.
func lastEdgeKey(t *testing.T, source string) string {
	t.Helper()
	g := compileD2ForPins(t, source)
	e := g.Edges[len(g.Edges)-1]
	return e.Src.AbsID() + " " + e.ArrowString() + " " + e.Dst.AbsID()
}

func routeString(e *d2graph.Edge) string {
	var b strings.Builder
	for _, p := range e.Route {
		fmt.Fprintf(&b, "(%v,%v) ", p.X, p.Y)
	}
	return fmt.Sprintf("%s src %v %vx%v dst %v %vx%v route %s", e.AbsID(), e.Src.TopLeft, e.Src.Width, e.Src.Height, e.Dst.TopLeft, e.Dst.Width, e.Dst.Height, b.String())
}

func TestEdgePortsLayout(t *testing.T) {
	for _, tc := range edgePortCases {
		for _, seed := range []int64{1, 2, 3} {
			t.Run(fmt.Sprintf("%s/seed %d", tc.name, seed), func(t *testing.T) {
				g, _ := pinEdge(t, tc.source, tc.src, tc.dst)
				if err := Layout(t.Context(), g, &Options{Seeds: []int64{seed}, MaxConcurrency: 1}); err != nil {
					t.Fatal(err)
				}
				// Every pinned edge, including those pinned in the source.
				for _, e := range g.Edges {
					if err := e.PortsHonored(); err != nil {
						t.Errorf("%v\n%s", err, routeString(e))
					}
				}
			})
		}
	}
}

func TestEdgePortsRouteEdges(t *testing.T) {
	for _, tc := range edgePortCases {
		t.Run(tc.name, func(t *testing.T) {
			// Lay out without pins, then pin the edge and reroute only it, which
			// is what an editor does when a user drags a connection to a handle.
			g := compileD2ForPins(t, tc.source)
			if err := Layout(t.Context(), g, &Options{Seeds: []int64{1}, MaxConcurrency: 1}); err != nil {
				t.Fatal(err)
			}
			e := g.Edges[len(g.Edges)-1]
			if tc.src != "" {
				e.SrcPort = &d2graph.Scalar{Value: tc.src}
			}
			if tc.dst != "" {
				e.DstPort = &d2graph.Scalar{Value: tc.dst}
			}
			if err := RouteEdges(t.Context(), g, []*d2graph.Edge{e}); err != nil {
				t.Fatal(err)
			}
			if err := e.PortsHonored(); err != nil {
				t.Errorf("%v\n%s", err, routeString(e))
			}
		})
	}
}

func TestEdgePortsManyOnOneSide(t *testing.T) {
	// TALA may let edges pinned to the same side share a trunk, as it does
	// for any fan-out, but each one must still leave through that side.
	g := compileD2ForPins(t, `a; b; c; d
a -> b: {source-port: right}
a -> c: {source-port: right}
a -> d: {source-port: right; target-port: right}
`)
	if err := Layout(t.Context(), g, &Options{Seeds: []int64{1}, MaxConcurrency: 1}); err != nil {
		t.Fatal(err)
	}
	for _, e := range g.Edges {
		if err := e.PortsHonored(); err != nil {
			t.Errorf("%v\n%s", err, routeString(e))
		}
	}
}

func TestEdgePortsTranslation(t *testing.T) {
	g := compileD2ForPins(t, `a -> b: {source-port: top; target-port: left}
c <- d: {source-port: bottom; target-port: right}
e <-> f: {source-port: top}
`)
	input, err := newSeedInput(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	sides := map[string][2]string{}
	for _, edge := range input.graph.Edges {
		side := func(o *geo.Orientation) string {
			if o == nil {
				return ""
			}
			return o.ToString()
		}
		key := fmt.Sprintf("%v->%v", *edge.From.D2ID, *edge.To.D2ID)
		sides[key] = [2]string{side(edge.FromSide), side(edge.ToSide)}
	}
	want := map[string][2]string{
		"a->b": {"Top", "Left"},
		// TALA routes c <- d as d -> c, so the pins swap with the endpoints.
		"d->c": {"Right", "Bottom"},
		"e->f": {"Top", ""},
	}
	for k, w := range want {
		if sides[k] != w {
			t.Errorf("%s: sides %v, want %v (all: %v)", k, sides[k], w, sides)
		}
	}
}
