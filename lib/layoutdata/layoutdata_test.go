package layoutdata

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParsePadding(t *testing.T) {
	tests := []struct {
		in   string
		want Sides
		err  string
	}{
		{in: "15", want: Sides{15, 15, 15, 15}},
		{in: " 0 ", want: Sides{}},
		{in: "[top=1,left=2,bottom=3,right=4]", want: Sides{1, 2, 3, 4}},
		{in: "[ top = 1 , right = 4 ]", want: Sides{Top: 1, Right: 4}},
		{in: "left=7", want: Sides{Left: 7}},
		{in: "[TOP=9]", want: Sides{Top: 9}},
		{in: "-3", err: "-3 is not between 0 and 10000"},
		{in: "[top]", err: `"top" is not side=pixels`},
		{in: "[top=-3]", err: "top: -3 is not between 0"},
		{in: "[top=x]", err: `top: "x" is not a whole number`},
		{in: "[top=1.5]", err: "not a whole number"},
		{in: "[middle=4]", err: `"middle" is not a side`},
		{in: "[top=1,top=2]", err: "top is given twice"},
		{in: "[]", err: "no sides given"},
		{in: "", err: "no sides given"},
		{in: "[top=10001]", err: "not between 0 and 10000"},
	}
	for _, tt := range tests {
		got, err := ParsePadding(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("ParsePadding(%q) error = %v, want %q", tt.in, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParsePadding(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}
	if got := (Sides{1, 2, 3, 4}).String(); got != "[top=1,left=2,bottom=3,right=4]" {
		t.Errorf("String() = %q", got)
	}
}

func TestInt(t *testing.T) {
	for _, v := range []any{"30", 30, int64(30), float64(30), json.Number("30")} {
		got, ok, err := Int(map[string]any{ELKNodeNode: v}, ELKNodeNode)
		if err != nil || !ok || got != 30 {
			t.Errorf("Int(%#v) = %d, %v, %v; want 30", v, got, ok, err)
		}
	}
	for _, v := range []any{"-3", "lol", "1.5", 1.5, -1, "10001", []any{"1"}, true, nil} {
		_, ok, err := Int(map[string]any{ELKNodeNode: v}, ELKNodeNode)
		if err == nil || ok {
			t.Errorf("Int(%#v) succeeded, want an error", v)
			continue
		}
		if !strings.Contains(err.Error(), `for "elk-nodeNode"`) {
			t.Errorf("Int(%#v) error = %v, want it to name the key", v, err)
		}
	}
	if _, ok, err := Int(map[string]any{"other": "1"}, ELKNodeNode); ok || err != nil {
		t.Errorf("missing key = %v, %v; want not set", ok, err)
	}
	if _, ok, err := Int(nil, ELKNodeNode); ok || err != nil {
		t.Errorf("nil data = %v, %v; want not set", ok, err)
	}
}

func TestValueIsCaseInsensitive(t *testing.T) {
	// D2 field names are case-insensitive, and the compiler keeps the spelling
	// of the first occurrence.
	got, ok, err := Int(map[string]any{"ELK-NodeNode": "5"}, ELKNodeNode)
	if err != nil || !ok || got != 5 {
		t.Fatalf("Int = %d, %v, %v; want 5", got, ok, err)
	}
	// An exact spelling wins over a different case.
	got, _, _ = Int(map[string]any{"ELK-NODENODE": "5", ELKNodeNode: "6"}, ELKNodeNode)
	if got != 6 {
		t.Fatalf("Int = %d, want the exact key's 6", got)
	}
	if spec, ok := Lookup("DAGRE-RANKSEP"); !ok || spec.Key != DagreRankSep {
		t.Fatalf("Lookup = %+v, %v", spec, ok)
	}
}

func TestPaddingValue(t *testing.T) {
	for in, want := range map[any]string{
		"15":                            "[top=15,left=15,bottom=15,right=15]",
		15:                              "[top=15,left=15,bottom=15,right=15]",
		"[top=8,right=2]":               "[top=8,left=0,bottom=0,right=2]",
		"top=1,left=2,bottom=3,right=4": "[top=1,left=2,bottom=3,right=4]",
	} {
		got, ok, err := PaddingValue(map[string]any{ELKPadding: in}, ELKPadding)
		if err != nil || !ok || got != want {
			t.Errorf("PaddingValue(%#v) = %q, %v, %v; want %q", in, got, ok, err, want)
		}
	}
	_, _, err := PaddingValue(map[string]any{ELKPadding: "[top=x]"}, ELKPadding)
	if err == nil || !strings.Contains(err.Error(), `for "elk-padding", got "[top=x]"`) {
		t.Errorf("error = %v", err)
	}
	_, _, err = PaddingValue(map[string]any{ELKPadding: []any{"1"}}, ELKPadding)
	if err == nil || !strings.Contains(err.Error(), `got "[1]"`) {
		t.Errorf("error = %v", err)
	}
}

func TestAlgorithm(t *testing.T) {
	for _, in := range []string{"layered", "mrtree", "org.eclipse.elk.force"} {
		got, ok, err := Algorithm(map[string]any{ELKAlgorithm: in}, ELKAlgorithm)
		if err != nil || !ok || got != in {
			t.Errorf("Algorithm(%q) = %q, %v, %v", in, got, ok, err)
		}
	}
	_, _, err := Algorithm(map[string]any{ELKAlgorithm: "disco"}, ELKAlgorithm)
	if err == nil || !strings.Contains(err.Error(), "expected one of layered,") {
		t.Errorf("error = %v", err)
	}
}

func TestInt64s(t *testing.T) {
	for _, in := range []any{[]any{"1", "-2"}, []int64{1, -2}, []any{1, -2}, []string{"1", "-2"}} {
		got, ok, err := Int64s(map[string]any{TALASeeds: in}, TALASeeds)
		if err != nil || !ok || !reflect.DeepEqual(got, []int64{1, -2}) {
			t.Errorf("Int64s(%#v) = %v, %v, %v", in, got, ok, err)
		}
	}
	for in, want := range map[string]any{
		"expected a list":    "1",
		"index 1 (nope)":     []any{1, "nope"},
		"signed 64-bit":      []string{"9223372036854775808"},
		"at most 64 entries": make([]int, MaxListEntries+1),
	} {
		_, _, err := Int64s(map[string]any{TALASeeds: want}, TALASeeds)
		if err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("Int64s(%#v) error = %v, want %q", want, err, in)
		}
	}
}

func TestCheckKey(t *testing.T) {
	for key, want := range map[string]string{
		"elk-nodeNode":         "",
		"Elk-NodeNode":         "",
		"tala-seeds":           "",
		"tala-anything":        "",
		"cat":                  "",
		"elkish":               "",
		"elk-nodeSpacing":      `"elk-nodeSpacing" is not a valid ELK option`,
		"elk-nodeNodeBetween":  `"elk-nodeNodeBetween" is not a valid ELK option, did you mean "elk-nodeNodeBetweenLayers"?`,
		"elk-edgenod":          `did you mean "elk-edgeNode"?`,
		"dagre-rankSeparation": `"dagre-rankSeparation" is not a valid dagre option`,
		"dagre-rnaksep":        `did you mean "dagre-ranksep"?`,
		"dagre-":               `"dagre-" is not a valid dagre option`,
	} {
		err := CheckKey(key)
		if want == "" {
			if err != nil {
				t.Errorf("CheckKey(%q) = %v, want nil", key, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CheckKey(%q) = %v, want %q", key, err, want)
		}
	}
}

func TestValidate(t *testing.T) {
	for _, spec := range Specs() {
		var good any = "10"
		switch spec.Kind {
		case ELKAlgorithmID:
			good = "layered"
		case Int64List:
			good = []any{"1"}
		case TALAQualityLevel:
			good = "fast"
		case Bool:
			good = "true"
		}
		if err := Validate(spec, good); err != nil {
			t.Errorf("Validate(%s, %#v) = %v", spec.Key, good, err)
		}
		if err := Validate(spec, map[string]any{}); err == nil {
			t.Errorf("Validate(%s, map) succeeded", spec.Key)
		}
	}
}

func TestValidateTALAQualityAndSeparateEdges(t *testing.T) {
	quality, _ := Lookup(TALAQuality)
	separate, _ := Lookup("TALA-Separate-Edges")
	for _, q := range []string{"full", "balanced", "fast"} {
		if err := Validate(quality, q); err != nil {
			t.Errorf("Validate(tala-quality, %q) = %v", q, err)
		}
	}
	for _, bad := range []any{"turbo", "", "FAST", []any{"fast"}} {
		if err := Validate(quality, bad); err == nil || !strings.Contains(err.Error(), `for "tala-quality"`) {
			t.Errorf("Validate(tala-quality, %#v) = %v", bad, err)
		}
	}
	for _, good := range []any{"true", "false", true, false} {
		if err := Validate(separate, good); err != nil {
			t.Errorf("Validate(tala-separate-edges, %#v) = %v", good, err)
		}
	}
	for _, bad := range []any{"yes", "1", "", []any{"true"}} {
		if err := Validate(separate, bad); err == nil || !strings.Contains(err.Error(), `expected true or false for "tala-separate-edges"`) {
			t.Errorf("Validate(tala-separate-edges, %#v) = %v", bad, err)
		}
	}
}

func TestSpecsMatchEngines(t *testing.T) {
	for _, spec := range Specs() {
		if got := Engine(spec.Key); got != spec.Engine {
			t.Errorf("Engine(%q) = %q, want %q", spec.Key, got, spec.Engine)
		}
	}
	if Engine("cat") != "" || Engine("elkish") != "" {
		t.Error("keys outside engine namespaces must not belong to an engine")
	}
}

func TestSignature(t *testing.T) {
	data := map[string]any{ELKNodeNode: "10", DagreRankSep: "60", "cat": "hat"}
	if got := Signature(data, "elk"); got != "elk-nodeNode=10;" {
		t.Errorf("elk signature = %q", got)
	}
	if got := Signature(data, "dagre"); got != "dagre-ranksep=60;" {
		t.Errorf("dagre signature = %q", got)
	}
	if got := Signature(nil, "elk"); got != "" {
		t.Errorf("empty signature = %q", got)
	}
}
