//go:build js && wasm

package d2wasm

import (
	"encoding/json"
	"strings"
	"sync"
	"syscall/js"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestRenderRejectsMalformedDiagramBeforeBoardTraversal(t *testing.T) {
	request := RenderRequest{
		Diagram: &d2target.Diagram{Layers: []*d2target.Diagram{nil}},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	call := wrapWASMCall(Render)
	defer call.Release()
	got := call.Invoke(string(encoded)).String()
	var response WASMResponse
	if err := json.Unmarshal([]byte(got), &response); err != nil {
		t.Fatalf("invalid response %q: %v", got, err)
	}
	if response.Error == nil || response.Error.Code != 400 || !strings.Contains(response.Error.Message, "root.layers[0] is nil") {
		t.Fatalf("error = %#v, want invalid-diagram response", response.Error)
	}
}

func TestRenderAnimationDefaultsMissingPadding(t *testing.T) {
	animateInterval := int64(1_000)
	request := RenderRequest{
		Diagram: d2target.NewDiagram(),
		Opts:    &RenderOptions{AnimateInterval: &animateInterval},
	}
	response := invokeRenderRequest(t, request)
	if response.Error != nil {
		t.Fatalf("Render() error = %#v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("Render() returned no animated SVG")
	}
}

func TestRenderRejectsAppendixAnimation(t *testing.T) {
	animateInterval := int64(1_000)
	forceAppendix := true
	request := RenderRequest{
		Diagram: d2target.NewDiagram(),
		Opts: &RenderOptions{
			AnimateInterval: &animateInterval,
			ForceAppendix:   &forceAppendix,
		},
	}
	response := invokeRenderRequest(t, request)
	if response.Error == nil || response.Error.Code != 400 || response.Error.Message != "forceAppendix is not supported for animated SVGs" {
		t.Fatalf("Render() error = %#v, want unsupported-options response", response.Error)
	}
}

func invokeRenderRequest(t *testing.T, request RenderRequest) WASMResponse {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	call := wrapWASMCall(Render)
	defer call.Release()
	got := call.Invoke(string(encoded)).String()
	var response WASMResponse
	if err := json.Unmarshal([]byte(got), &response); err != nil {
		t.Fatalf("invalid response %q: %v", got, err)
	}
	return response
}

func TestDeprecatedRawWASMCallsWarnOnceAndRemainCallable(t *testing.T) {
	t.Run("getObjOrder", func(t *testing.T) {
		getObjOrderDeprecationOnce = sync.Once{}
		warnings := captureWASMWarnings(t)
		call := wrapWASMCall(GetObjOrder)
		defer call.Release()

		got := call.Invoke("a: {\n  b\n}\nc").String()
		var response struct {
			Data struct {
				Order []string `json:"order"`
			} `json:"data"`
			Error *WASMError `json:"error"`
		}
		if err := json.Unmarshal([]byte(got), &response); err != nil {
			t.Fatalf("invalid response %q: %v", got, err)
		}
		if response.Error != nil {
			t.Fatalf("unexpected error: %v", response.Error)
		}
		wantOrder := []string{"a", "c", "a.b"}
		if len(response.Data.Order) != len(wantOrder) {
			t.Fatalf("order = %v, want %v", response.Data.Order, wantOrder)
		}
		for i := range wantOrder {
			if response.Data.Order[i] != wantOrder[i] {
				t.Fatalf("order = %v, want %v", response.Data.Order, wantOrder)
			}
		}

		got = call.Invoke().String()
		assertWASMError(t, got, "missing dsl argument", 400)
		assertSingleWASMWarning(t, *warnings, getObjOrderDeprecation)
	})

	t.Run("getELKGraph", func(t *testing.T) {
		getELKGraphDeprecationOnce = sync.Once{}
		warnings := captureWASMWarnings(t)
		call := wrapWASMCall(GetELKGraph)
		defer call.Release()

		got := call.Invoke(`{"fs":{"index":"x -> y"}}`).String()
		var response struct {
			Data  json.RawMessage `json:"data"`
			Error *WASMError      `json:"error"`
		}
		if err := json.Unmarshal([]byte(got), &response); err != nil {
			t.Fatalf("invalid response %q: %v", got, err)
		}
		if response.Error != nil {
			t.Fatalf("unexpected error: %v", response.Error)
		}
		if len(response.Data) == 0 || string(response.Data) == "null" {
			t.Fatalf("missing ELK graph in response: %s", got)
		}

		got = call.Invoke().String()
		assertWASMError(t, got, "missing JSON argument", 400)
		assertSingleWASMWarning(t, *warnings, getELKGraphDeprecation)
	})
}

func TestDeprecatedRawWASMWarningCannotChangeResponse(t *testing.T) {
	console := js.Global().Get("console")
	if console.Type() != js.TypeObject {
		t.Fatal("JavaScript console is unavailable")
	}
	originalWarn := console.Get("warn")
	warn := js.Global().Call("eval", `(function () { throw new Error("consumer console.warn failed"); })`)
	console.Set("warn", warn)
	t.Cleanup(func() {
		console.Set("warn", originalWarn)
	})

	var once sync.Once
	call := wrapWASMCall(func(args []js.Value) (interface{}, error) {
		warnDeprecatedWASMCall(&once, "deprecated")
		return "unchanged", nil
	})
	defer call.Release()

	got := call.Invoke().String()
	if got != `{"data":"unchanged"}` {
		t.Fatalf("response = %s, want legacy response unchanged", got)
	}
}

func TestRenderRejectsDangerousLegendShapeLinks(t *testing.T) {
	call := wrapWASMCall(Render)
	defer call.Release()

	for _, tc := range []struct {
		name    string
		request string
		object  string
	}{
		{
			name:    "shape",
			request: `{"diagram":{"legend":{"shapes":[{"id":"unsafe-shape","type":"rectangle","label":"Shape","opacity":1,"strokeWidth":2,"fill":"B6","stroke":"B1","link":"javascript:alert(1)"}]}},"options":{}}`,
			object:  `legend shape "unsafe-shape"`,
		},
		{
			name:    "image shape",
			request: `{"diagram":{"legend":{"shapes":[{"id":"unsafe-image","type":"image","label":"Image","opacity":1,"icon":{"Scheme":"https","Host":"example.com","Path":"/image.png"},"link":"data:text/html,<script>alert(1)</script>"}]}},"options":{}}`,
			object:  `legend shape "unsafe-image"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := call.Invoke(tc.request).String()
			var got WASMResponse
			if err := json.Unmarshal([]byte(response), &got); err != nil {
				t.Fatalf("invalid response %q: %v", response, err)
			}
			if got.Error == nil || got.Error.Code != 500 || !strings.Contains(got.Error.Message, tc.object+" uses an unsafe link URL scheme") {
				t.Fatalf("error = %#v, want code 500 containing unsafe-link error for %s", got.Error, tc.object)
			}
		})
	}
}

func captureWASMWarnings(t *testing.T) *[]string {
	t.Helper()
	console := js.Global().Get("console")
	if console.Type() != js.TypeObject {
		t.Fatal("JavaScript console is unavailable")
	}
	originalWarn := console.Get("warn")
	warnings := make([]string, 0, 1)
	warn := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			warnings = append(warnings, args[0].String())
		}
		return nil
	})
	console.Set("warn", warn)
	t.Cleanup(func() {
		console.Set("warn", originalWarn)
		warn.Release()
	})
	return &warnings
}

func assertWASMError(t *testing.T, response, wantMessage string, wantCode int) {
	t.Helper()
	var got WASMResponse
	if err := json.Unmarshal([]byte(response), &got); err != nil {
		t.Fatalf("invalid response %q: %v", response, err)
	}
	if got.Error == nil {
		t.Fatalf("missing error in response: %s", response)
	}
	if got.Error.Message != wantMessage || got.Error.Code != wantCode {
		t.Fatalf("error = %#v, want message %q and code %d", got.Error, wantMessage, wantCode)
	}
}

func assertSingleWASMWarning(t *testing.T, warnings []string, want string) {
	t.Helper()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %q, want exactly one warning", warnings)
	}
	if warnings[0] != want {
		t.Fatalf("warning = %q, want %q", warnings[0], want)
	}
}

func compileForTest(t *testing.T, options string) (CompileResponse, *WASMError) {
	t.Helper()
	call := wrapWASMCall(Compile)
	defer call.Release()
	got := call.Invoke(`{"fs":{"index":"a -> b\nb -> c\na -> c\n"},"options":` + options + `}`).String()
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

func shapeX(r CompileResponse, id string) int {
	for _, s := range r.Diagram.Shapes {
		if s.ID == id {
			return s.Pos.X
		}
	}
	return -1
}

func TestCompileLayoutOptions(t *testing.T) {
	base, werr := compileForTest(t, `{"layout":"dagre"}`)
	if werr != nil {
		t.Fatalf("dagre: %v", werr.Message)
	}
	wide, werr := compileForTest(t, `{"layout":"dagre","dagre":{"nodeSep":300}}`)
	if werr != nil {
		t.Fatalf("dagre wide: %v", werr.Message)
	}
	if shapeX(base, "c") == shapeX(wide, "c") && shapeX(base, "b") == shapeX(wide, "b") {
		t.Fatalf("dagre.nodeSep had no effect")
	}
	elk, werr := compileForTest(t, `{"layout":"elk"}`)
	if werr != nil {
		t.Fatalf("elk: %v", werr.Message)
	}
	elkWide, werr := compileForTest(t, `{"layout":"elk","elk":{"nodeSpacing":400,"padding":"[top=10,left=10,bottom=10,right=10]"}}`)
	if werr != nil {
		t.Fatalf("elk wide: %v", werr.Message)
	}
	same := true
	for i := range elk.Diagram.Shapes {
		if elk.Diagram.Shapes[i].Pos != elkWide.Diagram.Shapes[i].Pos {
			same = false
		}
	}
	if same {
		t.Fatalf("elk options had no effect")
	}
	if _, werr := compileForTest(t, `{"layout":"dagre","dagre":{"nodeSep":-1}}`); werr == nil || werr.Code != 400 {
		t.Fatalf("negative spacing should be rejected, got %v", werr)
	}
	if _, werr := compileForTest(t, `{"layout":"tala","tala":{"seeds":[7]}}`); werr != nil {
		t.Fatalf("tala seeds: %v", werr.Message)
	}
}
