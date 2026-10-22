package layoutgraph

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

func TestIconReservedDims(t *testing.T) {
	var none *Icon
	if none.HasExplicitSize() {
		t.Fatal("nil icon has an explicit size")
	}
	auto := &Icon{}
	if w, h := auto.ReservedDims(); w != MaxIconSize || h != MaxIconSize {
		t.Fatalf("automatic reserved size %vx%v", w, h)
	}
	explicit := &Icon{Width: 120, Height: 40}
	if w, h := explicit.ReservedDims(); w != 120 || h != 40 {
		t.Fatalf("explicit reserved size %vx%v", w, h)
	}
}

func TestNodeIconDimsAndBox(t *testing.T) {
	node := NewNode(1, 200, 200)
	node.TopLeft = geo.NewPoint(0, 0)
	node.SetShape(shape.CIRCLE_TYPE)
	node.InitIcon()

	size := node.IconSize(label.InsideTopLeft)
	if w, h := node.IconDims(label.InsideTopLeft); w != size || h != size {
		t.Fatalf("automatic icon dims %vx%v, want %v", w, h, size)
	}

	node.Icon.Width, node.Icon.Height = 48, 24
	if w, h := node.IconDims(label.InsideTopLeft); w != 48 || h != 24 {
		t.Fatalf("explicit icon dims %vx%v", w, h)
	}

	// Border icons sit on the node's box like border labels in the
	// renderers, not on the circle's inscribed square.
	box := node.IconBox(label.BorderTopCenter)
	if box.TopLeft.X != 76 || box.TopLeft.Y != -12 || box.Width != 48 || box.Height != 24 {
		t.Fatalf("border-top-center icon box %s", box.ToString())
	}
	inner := node.InnerBox()
	if inner.TopLeft.Y <= 0 {
		t.Fatalf("circle inner box %s starts at the top", inner.ToString())
	}
	// inside positions stay in the inner box
	box = node.IconBox(label.InsideTopLeft)
	if box.TopLeft.X != inner.TopLeft.X+label.PADDING || box.TopLeft.Y != inner.TopLeft.Y+label.PADDING {
		t.Fatalf("inside-top-left icon box %s, inner box %s", box.ToString(), inner.ToString())
	}
}

func TestContainerPaddingUsesExplicitIconSize(t *testing.T) {
	g := NewGraph()
	container := NewNode(1, 300, 200)
	container.TopLeft = geo.NewPoint(0, 0)
	container.InitIcon()
	g.AddNodeUnchecked(container)
	g.AddNodeToContainer(nil, container)

	auto := g.containerPadding(container, false)
	if auto.top != MaxIconSize+2*label.PADDING {
		t.Fatalf("automatic icon padding %v", auto.top)
	}
	container.Icon.Width, container.Icon.Height = 120, 30
	explicit := g.containerPadding(container, false)
	if explicit.top != 120+2*label.PADDING || explicit.left != 120+2*label.PADDING {
		t.Fatalf("explicit icon padding %+v", explicit)
	}
	container.Icon.Width, container.Icon.Height = 16, 16
	small := g.containerPadding(container, false)
	// a small icon never takes away the default container padding
	if small.top != ContainerPadding {
		t.Fatalf("small icon padding %+v, want %v", small, ContainerPadding)
	}
}

func TestCopyIconRecordKeepsExplicitSize(t *testing.T) {
	source := &Icon{Position: label.BorderTopLeft, Width: 48, Height: 24}
	source.FixPosition()
	cloned := copyIconRecord(source)
	if cloned == source || cloned.Position != label.BorderTopLeft || cloned.Width != 48 || cloned.Height != 24 {
		t.Fatalf("cloned icon %+v", cloned)
	}
}
