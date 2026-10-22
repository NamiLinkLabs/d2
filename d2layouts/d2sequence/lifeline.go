package d2sequence

import (
	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2target"
)

// IsLifeline reports whether c is the synthetic lifeline of a sequence diagram
// actor in an exported board: a straight, two-point route from the actor down
// to the actor's synthetic end point, without arrowheads, drawn at
// LIFELINE_Z_INDEX. isShape reports whether an ID names a shape of the same
// board; the actor must be one and the end point must not.
//
// Z indices alone do not tell lifelines apart from ordinary connections in a
// mixed ordinary/sequence diagram, so the end point's ID is checked too.
func IsLifeline(c d2target.Connection, isShape func(id string) bool) bool {
	if c.ZIndex != LIFELINE_Z_INDEX || c.SrcArrow != d2target.NoArrowhead || c.DstArrow != d2target.NoArrowhead || len(c.Route) != 2 {
		return false
	}
	if !isShape(c.Src) || isShape(c.Dst) {
		return false
	}
	// Src is formatted D2 syntax, not the decoded IDVal. The synthetic end ID
	// keeps quoting around dots, quotes and escapes in the actor's own key.
	key, err := d2parser.ParseKey(c.Src)
	if err != nil || len(key.Path) == 0 {
		return false
	}
	local := d2format.Format(&d2ast.KeyPath{Path: key.Path[len(key.Path)-1:]})
	return c.Dst == LifelineEndID(local)
}
