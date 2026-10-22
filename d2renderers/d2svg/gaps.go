package d2svg

import (
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2renderers/internal/crossgaps"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
)

// gapMaskID names the mask of the n-th connection, in draw order, that gets
// gaps where others cross it.
func gapMaskID(diagramHash string, n int) string {
	return diagramHash + "-gap-" + strconv.Itoa(n)
}

// gapMask replaces the shared connection mask (labelMaskID) on a connection
// that gets gaps. It keeps that mask's label holes and adds one stroke along
// the connection crossing over at each gap, so the gap stays empty whatever is
// drawn behind it. Its region covers only the connection, which keeps it cheap
// for viewers to paint.
func gapMask(id, labelMaskID string, connection *d2target.Connection, gaps []crossgaps.Gap, sketch bool) string {
	x, y, width, height := crossgaps.Region(connection, sketch)
	region := []svg.Attribute{svg.FloatAttr("x", x), svg.FloatAttr("y", y), svg.FloatAttr("width", width), svg.FloatAttr("height", height)}
	var b strings.Builder
	b.WriteString(svg.OpenElement("mask", append([]svg.Attribute{
		svg.Attr("id", svg.LiteralID(id).String()),
		svg.Attr("maskUnits", "userSpaceOnUse"),
	}, region...)...))
	b.WriteString(svg.OpenElement("g", svg.Attr("mask", svg.LocalIRI(svg.LiteralID(labelMaskID)))))
	b.WriteString(svg.EmptyElement("rect", append(region, svg.Attr("fill", "white"))...))
	b.WriteString(svg.CloseElement("g"))
	for _, gap := range gaps {
		var d strings.Builder
		for i, p := range gap.Piece {
			if i == 0 {
				d.WriteString("M ")
			} else {
				d.WriteString(" L ")
			}
			d.WriteString(svg.FormatFloat(p.X))
			d.WriteByte(' ')
			d.WriteString(svg.FormatFloat(p.Y))
		}
		b.WriteString(svg.EmptyElement("path",
			svg.Attr("d", d.String()),
			svg.Attr("fill", "none"),
			svg.Attr("stroke", "black"),
			svg.FloatAttr("stroke-width", gap.Width),
			svg.Attr("stroke-linecap", "butt"),
		))
	}
	b.WriteString(svg.CloseElement("mask"))
	return b.String()
}
