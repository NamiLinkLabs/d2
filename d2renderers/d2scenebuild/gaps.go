package d2scenebuild

import (
	"fmt"
	"image/color"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/internal/crossgaps"
	"github.com/d2lang/d2/d2target"
)

// findConnectionGaps finds where connections cross when the options ask for
// gaps there, exactly as the SVG renderer does.
func (b *builder) findConnectionGaps() error {
	if !b.options.IntersectionGaps {
		return nil
	}
	gaps, err := crossgaps.Find(b.ctx, b.diagram, crossgaps.Options{Sketch: b.options.Sketch})
	if err != nil {
		return err
	}
	b.connectionGaps = gaps
	return nil
}

// withGaps wraps a connection's line and arrowheads in a node whose mask
// leaves out the gaps, the same mask the SVG renderer writes. It keeps
// everything else of the connection: a white backdrop over its region, then
// one black stroke along the connection crossing over at each gap.
func (b *builder) withGaps(connection *d2target.Connection, geometry []*d2scene.Node) []*d2scene.Node {
	gaps := b.connectionGaps[connection.ID]
	if len(gaps) == 0 {
		return geometry
	}
	black := d2scene.SolidPaint{Color: color.NRGBA{A: 0xff}}
	white := d2scene.SolidPaint{Color: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}}
	x, y, width, height := crossgaps.Region(connection, b.options.Sketch)

	root := d2scene.NewNode(nil)
	root.ID = connection.ID + ":gap-mask"
	base := d2scene.NewNode(d2scene.Rect{Box: d2scene.Box{X: x, Y: y, Width: width, Height: height}, Fill: white})
	base.ID = root.ID + ":base"
	root.Children = append(root.Children, base)
	for i, gap := range gaps {
		path := d2scene.Path{Stroke: &d2scene.Stroke{Paint: black, Width: gap.Width, Cap: d2scene.CapButt, Join: d2scene.JoinMiter, MiterLimit: 4}}
		for k, p := range gap.Piece {
			if k == 0 {
				path.Commands = append(path.Commands, d2scene.MoveTo(p.X, p.Y))
			} else {
				path.Commands = append(path.Commands, d2scene.LineTo(p.X, p.Y))
			}
		}
		cut := d2scene.NewNode(path)
		cut.ID = fmt.Sprintf("%s:%d", root.ID, i)
		root.Children = append(root.Children, cut)
	}

	node := d2scene.NewNode(nil)
	node.ID = connection.ID + ":gaps"
	node.Mask = &d2scene.Mask{Type: d2scene.MaskLuminance, Root: root, Transform: d2scene.Identity()}
	node.Children = geometry
	return []*d2scene.Node{node}
}
