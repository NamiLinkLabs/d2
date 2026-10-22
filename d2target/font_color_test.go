package d2target

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShapeGetFontColor(t *testing.T) {
	tests := []struct {
		name  string
		shape Shape
		want  string
	}{
		{"rectangle default", Shape{Type: ShapeRectangle}, "N1"},
		{"rectangle theme code", Shape{Type: ShapeRectangle, Text: Text{Color: "B2"}}, "B2"},
		{"table default uses stroke", Shape{Type: ShapeSQLTable, Stroke: "N7", Text: Text{Color: "N1"}}, "N7"},
		{"table italic default uses stroke", Shape{Type: ShapeSQLTable, Stroke: "B1", Text: Text{Color: "N2"}}, "B1"},
		{"class hex font color", Shape{Type: ShapeClass, Stroke: "N7", Text: Text{Color: "#ff0000"}}, "#ff0000"},
		{"table explicit theme font color", Shape{Type: ShapeSQLTable, Stroke: "B1", Text: Text{Color: "N7"}, ExplicitThemeFontColor: true}, "N7"},
		{"class explicit theme font color", Shape{Type: ShapeClass, Stroke: "N7", Text: Text{Color: "N1"}, ExplicitThemeFontColor: true}, "N1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.shape.GetFontColor(); got != tt.want {
				t.Errorf("GetFontColor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExplicitThemeFontColorJSON(t *testing.T) {
	b, err := json.Marshal(Shape{Type: ShapeSQLTable})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "explicitThemeFontColor") {
		t.Fatalf("unset explicitThemeFontColor must be omitted: %s", b)
	}

	var s Shape
	if err := json.Unmarshal([]byte(`{"type":"sql_table","stroke":"B1","color":"N7","explicitThemeFontColor":true}`), &s); err != nil {
		t.Fatal(err)
	}
	if got := s.GetFontColor(); got != "N7" {
		t.Fatalf("GetFontColor() after JSON round trip = %q, want N7", got)
	}
}
