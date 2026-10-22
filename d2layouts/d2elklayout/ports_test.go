package d2elklayout

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/textmeasure"
)

func compileForPorts(t *testing.T, source string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("", strings.NewReader(source), &d2compiler.CompileOptions{Context: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	return g
}

func portRouteString(e *d2graph.Edge) string {
	var b strings.Builder
	for _, p := range e.Route {
		fmt.Fprintf(&b, "(%v,%v) ", p.X, p.Y)
	}
	return fmt.Sprintf("%s src %v %vx%v dst %v %vx%v route %s", e.AbsID(), e.Src.TopLeft, e.Src.Width, e.Src.Height, e.Dst.TopLeft, e.Dst.Width, e.Dst.Height, b.String())
}

func TestEdgePorts(t *testing.T) {
	cases := []struct {
		name, source string
		// pinned lists the edges (by index) the test expects to honour pins.
		pinned []int
	}{
		{name: "facing", source: "a -> b: {source-port: right; target-port: left}"},
		{name: "same side", source: "a -> b: {source-port: top; target-port: top}"},
		{name: "adjacent sides", source: "a -> b: {source-port: left; target-port: bottom}"},
		{name: "reversed arrow", source: "a <- b: {source-port: bottom; target-port: right}"},
		{name: "source only", source: "a -> b: {source-port: left}"},
		{name: "direction right", source: "direction: right\na -> b: {source-port: bottom; target-port: bottom}"},
		{name: "direction up", source: "direction: up\na -> b -> c\na -> c: {source-port: left; target-port: left}"},
		{name: "fan out", source: "a -> b\na -> c\na -> d: {source-port: right; target-port: left}"},
		{name: "mixed pinned and free", source: "a -> b\na -> c: {source-port: left}\nd -> a\nd -> c: {target-port: right}", pinned: []int{1, 3}},
		{name: "container child", source: "x: {a -> b}\nc\nx.a -> c: {source-port: left; target-port: right}"},
		{name: "circle to diamond", source: "a: {shape: circle}\nb: {shape: diamond}\na -> b: {source-port: right; target-port: left}"},
		{name: "container to container", source: "x: {a -> b}\ny: {c}\nx -> y: {source-port: right; target-port: top}"},
		{name: "children of two containers", source: "x: {a -> b}\ny: {c}\nx.a -> y.c: {source-port: left; target-port: left}\nx -> y", pinned: []int{1}},
		{name: "sql_table", source: "t: {shape: sql_table; id: int}\nu\nt.id -> u\nt -> u: {source-port: right; target-port: right}"},
		{name: "sql_table side", source: "t: {shape: sql_table; id: int}\nu\nu -> t: {target-port: bottom}\nt -> u: {source-port: left; target-port: top}"},
		{name: "three on one side", source: "a -> b: {source-port: right}\na -> b: {source-port: right}\na -> b: {source-port: right}", pinned: []int{0, 1, 2}},
		{name: "self loop", source: "a -> a: {source-port: right; target-port: top}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := compileForPorts(t, tc.source)
			if err := DefaultLayout(context.Background(), g); err != nil {
				t.Fatal(err)
			}
			pinned := tc.pinned
			if pinned == nil {
				pinned = []int{len(g.Edges) - 1}
			}
			for _, i := range pinned {
				e := g.Edges[i]
				if !e.HasPort() {
					t.Fatalf("edge %s has no pin", e.AbsID())
				}
				if err := e.PortsHonored(); err != nil {
					t.Errorf("%v\n%s", err, portRouteString(e))
				}
			}
		})
	}
}

func TestEdgePortsSpreadOnOneSide(t *testing.T) {
	g := compileForPorts(t, `a -> b: {source-port: right}
a -> c: {source-port: right}
a -> d: {source-port: right}
`)
	if err := DefaultLayout(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	a := g.Objects[0]
	if a.Height < 3*port_spacing {
		t.Errorf("a is %v high, want room for three ports (%v)", a.Height, 3*port_spacing)
	}
	starts := map[float64]string{}
	for _, e := range g.Edges {
		if err := e.PortsHonored(); err != nil {
			t.Errorf("%v\n%s", err, portRouteString(e))
		}
		y := e.Route[0].Y
		if other, ok := starts[y]; ok {
			t.Errorf("%s and %s both leave a at y=%v", other, e.AbsID(), y)
		}
		starts[y] = e.AbsID()
	}
}

func TestEdgePortsLeaveUnpinnedGraphsAlone(t *testing.T) {
	g := compileForPorts(t, "a -> b\nt: {shape: sql_table; id: int}\nt -> a")
	stats := collectELKGraphStats(g)
	if len(stats.pinnedPorts) != 0 {
		t.Fatalf("pinned ports = %v, want none", stats.pinnedPorts)
	}
	if err := DefaultLayout(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if g.Objects[0].Width != 53 {
		// a's size comes only from its label; pins must not grow it.
		t.Errorf("a width = %v", g.Objects[0].Width)
	}
}
