package d2isometricimg

import (
	"image/color"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
)

// An explicit theme color code on the board style is the user's background,
// resolved through the active theme; an unmarked one is the theme default the
// isometric renderer replaces with its own ground color.
func TestRootThemeColorCodeBackground(t *testing.T) {
	isoDefault := color.NRGBA{245, 247, 251, 255}
	for _, tc := range []struct {
		name     string
		fill     string
		explicit bool
		theme    int64
		want     *color.NRGBA
	}{
		{name: "default theme fill", fill: "N7", want: &isoDefault},
		{name: "explicit code", fill: "AA2", explicit: true},
		{name: "explicit code dark theme", fill: "N7", explicit: true, theme: 200},
		{name: "literal hex", fill: "#4A6FF3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := sourcePanelFixture(t, "txtar/theme-color-codes/dagre/board.exp.json")
			if d.Config == nil {
				d.Config = &d2target.Config{}
			}
			d.Config.ThemeID = &tc.theme
			d.Root.Fill, d.Root.ExplicitThemeFill = tc.fill, tc.explicit
			want := tc.want
			if want == nil {
				hex := d2themes.ResolveThemeColor(d2themescatalog.Find(tc.theme), tc.fill)
				c := nativePaint(hex, "#000000")
				want = &c
			}
			if got := nativeFixtureScene(t, d).background; got != *want {
				t.Fatalf("background %v, want %v", got, *want)
			}
		})
	}
}
