//go:build js && wasm

package d2wasm

import (
	"strings"
	"testing"

	"github.com/d2lang/util-go/go2"
)

func TestIntersectionGapsWASM(t *testing.T) {
	const crossing = "grid-columns: 3\na; b; c; d; e; f; g; h; i\nb -> h\nd -> f\n"
	for _, test := range []struct {
		name, source          string
		compileOpt, renderOpt *bool
		want                  bool
	}{
		{name: "default", source: crossing},
		{name: "compile option", source: crossing, compileOpt: go2.Pointer(true), want: true},
		{name: "source", source: "vars: {d2-config: {intersection-gaps: true}}\n" + crossing, want: true},
		{name: "compile option overrides source", source: "vars: {d2-config: {intersection-gaps: true}}\n" + crossing, compileOpt: go2.Pointer(false)},
		{name: "render option", source: crossing, renderOpt: go2.Pointer(true), want: true},
		{name: "render option overrides source", source: "vars: {d2-config: {intersection-gaps: true}}\n" + crossing, renderOpt: go2.Pointer(false)},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := CompileRequest{FS: map[string]string{"index": test.source}, Opts: &CompileOptions{RenderOptions: RenderOptions{IntersectionGaps: test.compileOpt}}}
			compiled, err := Compile(wasmJSONArgument(t, request))
			if err != nil {
				t.Fatal(err)
			}
			result := compiled.(CompileResponse)
			resolved := result.RenderOptions.IntersectionGaps
			if resolved == nil {
				t.Fatal("compile didn't resolve intersectionGaps")
			}
			opts := result.RenderOptions
			if test.renderOpt != nil {
				opts.IntersectionGaps = test.renderOpt
			} else if *resolved != test.want {
				t.Fatalf("resolved intersectionGaps = %v, want %v", *resolved, test.want)
			}
			out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &opts}))
			if err != nil {
				t.Fatal(err)
			}
			if gaps := strings.Contains(string(out.([]byte)), "-gap-0"); gaps != test.want {
				t.Fatalf("gaps = %v, want %v", gaps, test.want)
			}
		})
	}
}
