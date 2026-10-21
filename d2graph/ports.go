package d2graph

import (
	"fmt"
	"math"
	"strings"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// PortSideOrientation maps a source-port / target-port value ("top", "right",
// "bottom" or "left", any case) to the side it names.
func PortSideOrientation(side string) (geo.Orientation, bool) {
	switch strings.ToLower(side) {
	case "top":
		return geo.Top, true
	case "right":
		return geo.Right, true
	case "bottom":
		return geo.Bottom, true
	case "left":
		return geo.Left, true
	}
	return geo.NONE, false
}

// SrcPortSide reports the side of Src that the connection is pinned to with
// source-port.
func (e *Edge) SrcPortSide() (geo.Orientation, bool) {
	if e.SrcPort == nil {
		return geo.NONE, false
	}
	return PortSideOrientation(e.SrcPort.Value)
}

// DstPortSide reports the side of Dst that the connection is pinned to with
// target-port.
func (e *Edge) DstPortSide() (geo.Orientation, bool) {
	if e.DstPort == nil {
		return geo.NONE, false
	}
	return PortSideOrientation(e.DstPort.Value)
}

// HasPort reports whether either end of the connection is pinned to a side.
func (e *Edge) HasPort() bool {
	_, src := e.SrcPortSide()
	_, dst := e.DstPortSide()
	return src || dst
}

// PortSideNormal is the unit vector pointing out of a shape through side.
func PortSideNormal(side geo.Orientation) geo.Vector {
	switch side {
	case geo.Top:
		return geo.Vector{0, -1}
	case geo.Bottom:
		return geo.Vector{0, 1}
	case geo.Left:
		return geo.Vector{-1, 0}
	default:
		return geo.Vector{1, 0}
	}
}

// PortAnchor returns where a connection pinned to side meets the border of
// obj. along is the position on that side, from 0 (the left end of top and
// bottom, the top end of left and right) to 1; 0.5 is the middle.
//
// The side is the one of the shape's bounding box. For shapes that are not
// rectangles (circles, diamonds, clouds, ...) the anchor is moved inward,
// along the side's normal, onto the shape's own outline.
func (obj *Object) PortAnchor(side geo.Orientation, along float64) *geo.Point {
	along = math.Max(0, math.Min(1, along))
	tl := obj.TopLeft
	var p *geo.Point
	switch side {
	case geo.Top:
		p = geo.NewPoint(tl.X+obj.Width*along, tl.Y)
	case geo.Bottom:
		p = geo.NewPoint(tl.X+obj.Width*along, tl.Y+obj.Height)
	case geo.Left:
		p = geo.NewPoint(tl.X, tl.Y+obj.Height*along)
	default:
		p = geo.NewPoint(tl.X+obj.Width, tl.Y+obj.Height*along)
	}
	s := obj.ToShape()
	if s.Is("") || s.IsRectangular() {
		return p
	}
	// TraceToShapeBorder extends the segment from outside through the box and
	// keeps the outline crossing nearest to the box border.
	n := PortSideNormal(side)
	outside := geo.NewPoint(p.X+n[0]*obj.Width, p.Y+n[1]*obj.Height)
	return shape.TraceToShapeBorder(s, p, outside)
}

// PortsHonored reports whether e.Route honours source-port and target-port:
// a pinned end must lie on its side's half of the shape, and the route's first
// (or last) segment must run straight out of that side. It returns nil when
// nothing is pinned.
func (e *Edge) PortsHonored() error {
	if len(e.Route) < 2 {
		if e.HasPort() {
			return fmt.Errorf("%s: route has %d points", e.AbsID(), len(e.Route))
		}
		return nil
	}
	if side, ok := e.SrcPortSide(); ok {
		if err := portEndHonored(e.Src, side, e.Route[0], e.Route[1]); err != nil {
			return fmt.Errorf("%s: source-port %s: %w", e.AbsID(), e.SrcPort.Value, err)
		}
	}
	if side, ok := e.DstPortSide(); ok {
		n := len(e.Route)
		if err := portEndHonored(e.Dst, side, e.Route[n-1], e.Route[n-2]); err != nil {
			return fmt.Errorf("%s: target-port %s: %w", e.AbsID(), e.DstPort.Value, err)
		}
	}
	return nil
}

func portEndHonored(obj *Object, side geo.Orientation, end, next *geo.Point) error {
	const eps = 1.
	if obj.TopLeft == nil {
		return fmt.Errorf("%s has no position", obj.AbsID())
	}
	if end.X < obj.TopLeft.X-eps || end.X > obj.TopLeft.X+obj.Width+eps ||
		end.Y < obj.TopLeft.Y-eps || end.Y > obj.TopLeft.Y+obj.Height+eps {
		return fmt.Errorf("end %v is off %s", end, obj.AbsID())
	}
	n := PortSideNormal(side)
	center := obj.Center()
	if (end.X-center.X)*n[0]+(end.Y-center.Y)*n[1] <= 0 {
		return fmt.Errorf("end %v is not on that side of %s", end, obj.AbsID())
	}
	dx, dy := next.X-end.X, next.Y-end.Y
	along := dx*n[0] + dy*n[1]
	across := math.Abs(dx*n[1] - dy*n[0])
	if along <= 0 || across > eps {
		return fmt.Errorf("segment %v -> %v does not leave %s straight out of that side", end, next, obj.AbsID())
	}
	return nil
}
