package layoutgraph

import "github.com/d2lang/d2/lib/label"

// Icon is the icon-placement state attached to a layout node.
type Icon struct {
	Position label.Position
	// Width and Height are the explicit icon size (icon.width and
	// icon.height). 0 means the size follows the node, see Node.IconDims.
	Width, Height float64

	positionFixed bool
}

// HasExplicitSize reports whether the icon has an explicit width and height.
func (i *Icon) HasExplicitSize() bool {
	return i != nil && i.Width > 0 && i.Height > 0
}

// ReservedDims returns the icon size to reserve before the final node size
// is known: the explicit size, or the largest automatic size.
func (i *Icon) ReservedDims() (width, height float64) {
	if i.HasExplicitSize() {
		return i.Width, i.Height
	}
	return MaxIconSize, MaxIconSize
}

// PositionFixed reports whether Position was supplied by the caller and must
// be preserved by automatic icon placement.
func (i *Icon) PositionFixed() bool {
	return i.positionFixed
}

// FixPosition marks Position as supplied by the caller.
func (i *Icon) FixPosition() {
	i.positionFixed = true
}
