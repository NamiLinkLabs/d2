package geo

import (
	"math"
	"reflect"
	"testing"
)

func TestRouteCurve(t *testing.T) {
	t.Parallel()
	for n := 3; n <= 8; n++ {
		route := make(Route, n)
		for i := range route {
			// A staircase, so every point is a corner.
			route[i] = NewPoint(float64((i+1)/2)*100, float64(i/2)*100)
		}
		curve := route.Curve()
		if (len(curve)-1)%3 != 0 {
			t.Fatalf("%d points: curve has %d points, not 1 + 3 per cubic", n, len(curve))
		}
		if curve[0] != route[0] || curve[len(curve)-1] != route[n-1] {
			t.Fatalf("%d points: curve does not start and end with the route", n)
		}
		// The dagre layout has drawn this curve since its first version.
		if n == 3 {
			want := Route{route[0], NewPoint(80, 0), NewPoint(100, 20), route[2]}
			if !reflect.DeepEqual(curve, want) {
				t.Fatalf("curve = %v, want %v", curve, want)
			}
		}
	}
	for _, route := range []Route{nil, {NewPoint(0, 0)}, {NewPoint(0, 0), NewPoint(1, 1)}} {
		if curve := route.Curve(); !reflect.DeepEqual(curve, route) {
			t.Fatalf("a route without corners changed: %v", curve)
		}
	}
}

func TestRouteRoundCorners(t *testing.T) {
	t.Parallel()
	const radius = 20.
	routes := []Route{
		// A short orthogonal route of the kind an edge router returns, whose
		// plain Curve cuts its corners by half a segment.
		{NewPoint(0, 0), NewPoint(0, 100), NewPoint(300, 100), NewPoint(300, 200)},
		// A staircase, a short middle segment and a repeated point.
		{NewPoint(0, 0), NewPoint(100, 0), NewPoint(100, 10), NewPoint(100, 10), NewPoint(200, 10), NewPoint(200, 300)},
		{NewPoint(0, 0), NewPoint(50, 0), NewPoint(50, 50)},
	}
	for _, route := range routes {
		curve := route.RoundCorners(radius)
		if (len(curve)-1)%3 != 0 {
			t.Fatalf("%v: curve has %d points, not 1 + 3 per cubic", route, len(curve))
		}
		if !curve[0].Equals(route[0]) || !curve[len(curve)-1].Equals(route[len(route)-1]) {
			t.Fatalf("%v: curve does not start and end with the route", route)
		}
		for i := 0; i+3 < len(curve); i += 3 {
			bezier := NewBezierCurve(curve[i : i+4])
			for s := 0.; s <= 1; s += 1. / 64 {
				p := bezier.At(s)
				if d := distanceToRoute(p, route); d > radius/2+1e-9 {
					t.Fatalf("%v: curve point %v is %.2f from the route, more than %v", route, *p, d, radius/2)
				}
			}
		}
	}
	// The plain curve of the first route strays far from it.
	curve := routes[0].Curve()
	if d := distanceToRoute(NewBezierCurve(curve).At(.25), routes[0]); d < radius {
		t.Fatalf("Curve stays within %.2f of the route; the test no longer shows what RoundCorners fixes", d)
	}
	for _, route := range []Route{nil, {NewPoint(0, 0), NewPoint(1, 1)}, {NewPoint(0, 0), NewPoint(0, 0), NewPoint(1, 1)}} {
		if curve := route.RoundCorners(radius); !reflect.DeepEqual(curve, route) {
			t.Fatalf("a route without corners changed: %v", curve)
		}
	}
}

func distanceToRoute(p *Point, route Route) float64 {
	best := math.Inf(1)
	for i := 0; i+1 < len(route); i++ {
		a, b := route[i], route[i+1]
		ab := a.VectorTo(b)
		l2 := ab[0]*ab[0] + ab[1]*ab[1]
		t := 0.
		if l2 > 0 {
			t = math.Max(0, math.Min(1, ((p.X-a.X)*ab[0]+(p.Y-a.Y)*ab[1])/l2))
		}
		best = math.Min(best, EuclideanDistance(p.X, p.Y, a.X+t*ab[0], a.Y+t*ab[1]))
	}
	return best
}
