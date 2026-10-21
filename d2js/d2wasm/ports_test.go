//go:build js && wasm

package d2wasm

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func compileSourceForTest(t *testing.T, source, options string) (CompileResponse, *WASMError) {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"fs":      map[string]string{"index": source},
		"options": json.RawMessage(options),
	})
	if err != nil {
		t.Fatal(err)
	}
	call := wrapWASMCall(Compile)
	defer call.Release()
	got := call.Invoke(string(request)).String()
	var response struct {
		Data  *CompileResponse `json:"data"`
		Error *WASMError       `json:"error"`
	}
	if err := json.Unmarshal([]byte(got), &response); err != nil {
		t.Fatalf("invalid response: %v", err)
	}
	if response.Error != nil {
		return CompileResponse{}, response.Error
	}
	return *response.Data, nil
}

func TestCompileEdgePorts(t *testing.T) {
	const source = "a -> b: {source-port: top; target-port: right}\nb -> c\n"
	for _, layout := range []string{"dagre", "elk", "tala"} {
		t.Run(layout, func(t *testing.T) {
			response, werr := compileSourceForTest(t, source, `{"layout":"`+layout+`"}`)
			if werr != nil {
				t.Fatalf("compile: %d %s", werr.Code, werr.Message)
			}
			shapes := map[string]d2target.Shape{}
			for _, s := range response.Diagram.Shapes {
				shapes[s.ID] = s
			}
			var pinned, free *d2target.Connection
			for i := range response.Diagram.Connections {
				c := &response.Diagram.Connections[i]
				switch c.ID {
				case "(a -> b)[0]":
					pinned = c
				case "(b -> c)[0]":
					free = c
				}
			}
			if pinned == nil || free == nil {
				t.Fatalf("connections = %+v", response.Diagram.Connections)
			}
			if pinned.SrcPort != "top" || pinned.DstPort != "right" {
				t.Fatalf("srcPort/dstPort = %q/%q, want top/right", pinned.SrcPort, pinned.DstPort)
			}
			if free.SrcPort != "" || free.DstPort != "" {
				t.Fatalf("unpinned connection has ports %q/%q", free.SrcPort, free.DstPort)
			}
			route := pinned.Route
			a, b := shapes["a"], shapes["b"]
			// Shape positions are whole pixels; ELK's ports can sit half a pixel off.
			near := func(got float64, want int) bool { return math.Abs(got-float64(want)) <= 1 }
			if start := route[0]; !near(start.Y, a.Pos.Y) || route[1].Y >= start.Y || route[1].X != start.X {
				t.Errorf("route %v does not leave a (%+v) straight up", route, a.Pos)
			}
			n := len(route)
			if end := route[n-1]; !near(end.X, b.Pos.X+b.Width) || route[n-2].X <= end.X || route[n-2].Y != end.Y {
				t.Errorf("route %v does not enter b (%+v %dx%d) from the right", route, b.Pos, b.Width, b.Height)
			}
		})
	}
}

func TestRouteEdgesHonoursPorts(t *testing.T) {
	boxes := map[string]testBox{
		"a": {0, 0, 100, 60},
		"b": {300, 200, 100, 60},
	}
	const id = "(a -> b)[0]"
	response := mustRoute(t, "a -> b: {source-port: left; target-port: bottom}\n", boxes, id)
	route := response.Routes[id]
	if start := route[0]; start.X != 0 || route[1].X >= start.X || route[1].Y != start.Y {
		t.Errorf("route %v does not leave a straight out of its left side", route)
	}
	n := len(route)
	if end := route[n-1]; end.Y != 260 || route[n-2].Y <= end.Y || route[n-2].X != end.X {
		t.Errorf("route %v does not enter b straight through its bottom", route)
	}
	assertEnds(t, id, route, boxes["a"], boxes["b"])
}
