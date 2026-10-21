package geo

import (
	"math"
)

type Route []*Point

func (route Route) Length() float64 {
	l := 0.
	for i := 0; i < len(route)-1; i++ {
		l += EuclideanDistance(
			route[i].X, route[i].Y,
			route[i+1].X, route[i+1].Y,
		)
	}
	return l
}

// return the point at _distance_ along the route, and the index of the segment it's on
func (route Route) GetPointAtDistance(distance float64) (*Point, int) {
	remaining := distance
	var curr, next *Point
	var length float64
	for i := 0; i < len(route)-1; i++ {
		curr, next = route[i], route[i+1]
		length = EuclideanDistance(curr.X, curr.Y, next.X, next.Y)

		if remaining <= length {
			t := remaining / length
			// point t% of the way between curr and next
			return curr.Interpolate(next, t), i
		}
		remaining -= length
	}

	// distance > length, so continue with last segment
	// Note: distance < 0 handled above with first segment
	return curr.Interpolate(next, 1+remaining/length), len(route) - 2
}

func (route Route) GetBoundingBox() (tl, br *Point) {
	minX := math.Inf(1)
	minY := math.Inf(1)
	maxX := math.Inf(-1)
	maxY := math.Inf(-1)

	for _, p := range route {
		if p.X < minX {
			minX = p.X
		}
		if p.X > maxX {
			maxX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	return NewPoint(minX, minY), NewPoint(maxX, maxY)
}

// Curve returns a smooth path through the corners of route, as cubic Bézier
// curves: after the first point, every three points are the two control
// points and the end point of one curve. The path starts and ends where route
// does, and its control points lie on route's segments, so it bends inside
// each corner. This is the curve the dagre layout draws through its routes.
// A route of fewer than three points has no corner and is returned as it is.
func (route Route) Curve() Route {
	if len(route) < 3 {
		return route
	}
	vectors := make([]Vector, 0, len(route)-1)
	for i := 1; i < len(route); i++ {
		vectors = append(vectors, route[i-1].VectorTo(route[i]))
	}

	path := make(Route, 0, 3*len(route))
	path = append(path, route[0])
	path = append(path, route[0].AddVector(vectors[0].Multiply(.8)))
	for i := 1; i < len(vectors)-2; i++ {
		p := route[i]
		v := vectors[i]
		path = append(path, p.AddVector(v.Multiply(.2)))
		path = append(path, p.AddVector(v.Multiply(.5)))
		path = append(path, p.AddVector(v.Multiply(.8)))
	}
	path = append(path, route[len(route)-2].AddVector(vectors[len(vectors)-1].Multiply(.2)))
	path = append(path, route[len(route)-1])
	return path
}
