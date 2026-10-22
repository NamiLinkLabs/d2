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

// RoundCorners returns a smooth path that follows route and leaves it only
// to round each corner, within radius of that corner, as cubic Bézier curves
// in the form Curve returns. Curve alone cuts across a short route's corners
// by up to half of each segment, so it can cross what the route went around;
// RoundCorners adds points within radius of each corner first, so the curve
// stays within radius/2 of the route. Consecutive equal points count as one.
// A route of fewer than three distinct points is returned as it is.
func (route Route) RoundCorners(radius float64) Route {
	points := make(Route, 0, len(route))
	for _, p := range route {
		if len(points) == 0 || EuclideanDistance(points[len(points)-1].X, points[len(points)-1].Y, p.X, p.Y) > PRECISION {
			points = append(points, p)
		}
	}
	if len(points) < 3 {
		return route
	}
	dense := make(Route, 0, 3*len(points))
	dense = append(dense, points[0])
	for i := 1; i < len(points)-1; i++ {
		prev, corner, next := points[i-1], points[i], points[i+1]
		in := prev.VectorTo(corner)
		out := corner.VectorTo(next)
		rIn := math.Min(radius, in.Length()/2)
		rOut := math.Min(radius, out.Length()/2)
		dense = append(dense,
			corner.AddVector(in.Unit().Multiply(-rIn)),
			corner,
			corner.AddVector(out.Unit().Multiply(rOut)),
		)
	}
	dense = append(dense, points[len(points)-1])
	// A segment no longer than twice radius is split at its middle from both
	// of its corners; keep one point there, as a zero-length segment would
	// give the curve no direction.
	distinct := dense[:1]
	for _, p := range dense[1:] {
		if last := distinct[len(distinct)-1]; EuclideanDistance(last.X, last.Y, p.X, p.Y) > PRECISION {
			distinct = append(distinct, p)
		}
	}
	return distinct.Curve()
}
