package d2talalayout

import (
	"context"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/textmeasure"
)

func compileD2ForPins(t *testing.T, source string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("", strings.NewReader(source), &d2compiler.CompileOptions{Context: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	return g
}

// layoutPasses reports how many layout passes Layout needs for source's pins.
func layoutPasses(t *testing.T, source string) int {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, err := newSeedInput(ctx, compileD2ForPins(t, source))
	if err != nil {
		t.Fatal(err)
	}
	pins := newAbsolutePins(input.graph)
	if pins == nil {
		return 0
	}
	passes := 1
	for {
		best, err := layoutSeedInput(ctx, input, []int64{1}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !pins.correct(ctx, best.graph) {
			return passes
		}
		passes++
		if passes > 2 {
			t.Fatal("pins did not converge after one correction pass")
		}
	}
}

func TestAbsolutePinsPasses(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   int
	}{
		// No nested pin: the engine already matches D2 and nothing is rewritten.
		{"no pins", "a -> b\n", 0},
		{"root pin", "a -> b\nb: {top: 0; left: 600}\n", 0},
		// The default container padding is exact for ordinary containers.
		{"plain container", "g: {\n  p\n  q: {top: 500; left: 50}\n  p -> q\n}\n", 1},
		// Tall labels and icons enlarge the padding; one extra pass corrects it.
		{"tall label", "g: \"one\\ntwo\\nthree\\nfour\" {\n  p\n  q: {top: 400; left: 80}\n  p -> q\n}\n", 2},
		{"icon", "g: {\n  icon: https://icons.terrastruct.com/essentials/004-picture.svg\n  p\n  q: {top: 300; left: 200}\n  p -> q\n}\n", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := layoutPasses(t, tc.source); got != tc.want {
				t.Fatalf("got %d layout passes, want %d", got, tc.want)
			}
		})
	}
}

func TestAbsolutePinsImplicitContainerPins(t *testing.T) {
	ctx := context.Background()
	input, err := newSeedInput(ctx, compileD2ForPins(t, "o: {\n  g: {\n    p\n    q: {top: 500; left: 50}\n    r: {top: 520; left: 300}\n  }\n  s\n}\nx\n"))
	if err != nil {
		t.Fatal(err)
	}
	if newAbsolutePins(input.graph) == nil {
		t.Fatal("nested pins were not rewritten")
	}
	byID := map[string]*struct{ x, y float64 }{}
	pinned := map[string]bool{}
	for _, node := range input.graph.Nodes {
		if node.FixedTopLeft != nil {
			pinned[*node.D2ID] = true
			byID[*node.D2ID] = &struct{ x, y float64 }{node.FixedTopLeft.X, node.FixedTopLeft.Y}
		}
	}
	// Ancestors of pinned nodes become pinned; unrelated nodes stay free.
	for _, id := range []string{"o", "o.g", "o.g.q", "o.g.r"} {
		if !pinned[id] {
			t.Fatalf("%s is not pinned", id)
		}
	}
	for _, id := range []string{"o.s", "o.g.p", "x"} {
		if pinned[id] {
			t.Fatalf("%s is pinned", id)
		}
	}
	// The top-left-most pinned child sits at its container's interior origin,
	// and its siblings keep their absolute offsets from it.
	want := map[string][2]float64{
		"o.g.q": {0, 0},
		"o.g.r": {250, 20},
		"o.g":   {0, 0},
		"o":     {50 - 120, 500 - 120},
	}
	for id, w := range want {
		if got := byID[id]; got.x != w[0] || got.y != w[1] {
			t.Fatalf("%s engine pin (%v,%v), want (%v,%v)", id, got.x, got.y, w[0], w[1])
		}
	}
}
