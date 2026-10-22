package d2sequence_test

import (
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2sequence"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestLifelineEndIDArchitectureStable(t *testing.T) {
	const actorID = "a"
	const want = "a-lifeline-end-2251863791"

	if got := d2sequence.LifelineEndID(actorID); got != want {
		t.Fatalf("LifelineEndID(%q) = %q, want %q", actorID, got, want)
	}

	legacySignedID := "a-lifeline-end--2043103505"
	if d2sequence.IsLifelineEnd(&d2graph.Object{ID: legacySignedID}) {
		t.Fatalf("IsLifelineEnd accepted architecture-dependent ID %q", legacySignedID)
	}
}

func TestIsLifelineEnd(t *testing.T) {
	actorWithMarker := "actor-lifeline-end-archive"
	validID := d2sequence.LifelineEndID(actorWithMarker)

	tests := []struct {
		name string
		obj  *d2graph.Object
		want bool
	}{
		{name: "nil", obj: nil},
		{name: "valid", obj: &d2graph.Object{ID: validID}, want: true},
		{name: "wrong hash", obj: &d2graph.Object{ID: actorWithMarker + "-lifeline-end-0"}},
		{name: "ordinary object", obj: &d2graph.Object{ID: actorWithMarker}},
		{name: "graph member", obj: &d2graph.Object{ID: validID, Graph: &d2graph.Graph{}}},
		{name: "child", obj: &d2graph.Object{ID: validID, Parent: &d2graph.Object{}}},
		{name: "positioned", obj: &d2graph.Object{ID: validID, Box: geo.NewBox(nil, 1, 1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := d2sequence.IsLifelineEnd(test.obj); got != test.want {
				t.Fatalf("IsLifelineEnd(%#v) = %t, want %t", test.obj, got, test.want)
			}
		})
	}
}

func TestIsLifeline(t *testing.T) {
	shapes := map[string]bool{"a": true, `x."a.b"`: true, "b": true}
	isShape := func(id string) bool { return shapes[id] }
	lifeline := func(src, dst string) d2target.Connection {
		return d2target.Connection{
			Src: src, Dst: dst, ZIndex: d2sequence.LIFELINE_Z_INDEX,
			SrcArrow: d2target.NoArrowhead, DstArrow: d2target.NoArrowhead,
			Route: []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(0, 100)},
		}
	}
	tests := []struct {
		name string
		c    func() d2target.Connection
		want bool
	}{
		{name: "lifeline", c: func() d2target.Connection { return lifeline("a", d2sequence.LifelineEndID("a")) }, want: true},
		{name: "quoted nested actor", c: func() d2target.Connection {
			return lifeline(`x."a.b"`, d2sequence.LifelineEndID(`"a.b"`))
		}, want: true},
		{name: "other actor's end", c: func() d2target.Connection { return lifeline("b", d2sequence.LifelineEndID("a")) }},
		{name: "end is a shape", c: func() d2target.Connection { return lifeline("a", "b") }},
		{name: "unknown actor", c: func() d2target.Connection { return lifeline("c", d2sequence.LifelineEndID("c")) }},
		{name: "message z-index", c: func() d2target.Connection {
			c := lifeline("a", d2sequence.LifelineEndID("a"))
			c.ZIndex = d2sequence.MESSAGE_Z_INDEX
			return c
		}},
		{name: "arrowhead", c: func() d2target.Connection {
			c := lifeline("a", d2sequence.LifelineEndID("a"))
			c.DstArrow = d2target.ArrowArrowhead
			return c
		}},
		{name: "bent route", c: func() d2target.Connection {
			c := lifeline("a", d2sequence.LifelineEndID("a"))
			c.Route = append(c.Route, geo.NewPoint(10, 100))
			return c
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := d2sequence.IsLifeline(tc.c(), isShape); got != tc.want {
				t.Fatalf("IsLifeline = %v, want %v", got, tc.want)
			}
		})
	}
}
