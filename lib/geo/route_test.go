package geo

import (
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
