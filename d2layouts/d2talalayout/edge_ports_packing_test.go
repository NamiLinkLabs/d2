package d2talalayout_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/geo"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

// A pinned route runs out of its pinned side even when that side faces away
// from the other end. BinPack must keep other subgraphs (here the grid g,
// connected only through a cross-layout edge) off that side, or later route
// stages draw the route through them.
func TestPinnedRoutesAvoidPackedSubgraphs(t *testing.T) {
	source := `direction: right
a -> b: {source-port: bottom; target-port: bottom}
b -> c: {source-port: top; target-port: left}
a -> a: {source-port: right; target-port: top}
cont: {
  x -> y: {source-port: left; target-port: left}
  y -> x
}
c -> cont.x: {source-port: right; target-port: top}
n: near {near: top-center}
n -> a: {target-port: top}
g: {
  grid-rows: 2
  p; q; r; s
}
g.p -> g.s: {source-port: right; target-port: left}
a -> g.q: {source-port: left; target-port: right}
`
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []int64{1, 2, 3} {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			layout := "tala"
			ctx := d2log.With(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, g, err := d2lib.Compile(ctx, source, &d2lib.CompileOptions{
				Ruler:  ruler,
				Layout: &layout,
				LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
					return func(ctx context.Context, g *d2graph.Graph) error {
						return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{seed}, MaxConcurrency: 1})
					}, nil
				},
				RouterResolver: func(string) (d2graph.RouteEdges, error) { return d2talalayout.RouteEdges, nil },
			}, &d2svg.RenderOpts{})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range g.Edges {
				if strings.Contains(e.AbsID(), "(a -> b)") {
					assertRouteAvoidsOthers(t, g, e)
				}
				if err := e.PortsHonored(); err != nil && e.Src != e.Dst {
					t.Error(err)
				}
			}
		})
	}
}

func assertRouteAvoidsOthers(t *testing.T, g *d2graph.Graph, e *d2graph.Edge) {
	t.Helper()
	for _, obj := range g.Objects {
		if obj == e.Src || obj == e.Dst || e.Src.IsDescendantOf(obj) || e.Dst.IsDescendantOf(obj) {
			continue
		}
		inner := geo.NewBox(geo.NewPoint(obj.TopLeft.X+1, obj.TopLeft.Y+1), obj.Width-2, obj.Height-2)
		for i := 1; i < len(e.Route); i++ {
			p, q := e.Route[i-1], e.Route[i]
			if inner.Contains(p) || inner.Contains(q) || len(inner.Intersections(*geo.NewSegment(p, q))) > 0 {
				t.Errorf("%s segment %v -> %v crosses %s at %v %vx%v", e.AbsID(), p, q, obj.AbsID(), obj.TopLeft, obj.Width, obj.Height)
			}
		}
	}
}
