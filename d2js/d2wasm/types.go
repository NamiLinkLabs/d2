//go:build js && wasm

package d2wasm

import (
	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/sourcemeta"
)

type WASMResponse struct {
	Data  interface{} `json:"data,omitempty"`
	Error *WASMError  `json:"error,omitempty"`
}

type WASMError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *WASMError) Error() string {
	return e.Message
}

type RefRangesResponse struct {
	Ranges       []d2ast.Range `json:"ranges"`
	ImportRanges []d2ast.Range `json:"importRanges"`
}

type BoardPositionResponse struct {
	BoardPath []string `json:"boardPath"`
	Err       string   `json:"err"`
}

type CompileRequest struct {
	FS        map[string]string `json:"fs"`
	InputPath *string           `json:"inputPath"`
	Opts      *CompileOptions   `json:"options"`
}

type RenderOptions struct {
	Pad                *int64                   `json:"pad"`
	Sketch             *bool                    `json:"sketch"`
	Isometric          *bool                    `json:"isometric"`
	Center             *bool                    `json:"center"`
	ThemeID            *int64                   `json:"themeID"`
	DarkThemeID        *int64                   `json:"darkThemeID"`
	ThemeOverrides     *d2target.ThemeOverrides `json:"themeOverrides"`
	DarkThemeOverrides *d2target.ThemeOverrides `json:"darkThemeOverrides"`
	Scale              *float64                 `json:"scale"`
	ForceAppendix      *bool                    `json:"forceAppendix"`
	Target             *string                  `json:"target"`
	AnimateInterval    *int64                   `json:"animateInterval"`
	Salt               *string                  `json:"salt"`
	NoXMLTag           *bool                    `json:"noXMLTag"`
	ASCII              *bool                    `json:"ascii"`
	ASCIIMode          *string                  `json:"asciiMode"`
	// Source is embedded in the rendered SVG. Compile sets it when
	// CompileOptions.EmbedSource is set.
	Source *sourcemeta.Bundle `json:"source,omitempty"`
}

type CompileOptions struct {
	RenderOptions
	Layout *string `json:"layout"`
	// Engine tuning; only the options of the engine in use apply.
	ELK          *ELKOptions   `json:"elk"`
	Dagre        *DagreOptions `json:"dagre"`
	TALA         *TALAOptions  `json:"tala"`
	FontRegular  *[]byte       `json:"FontRegular"`
	FontItalic   *[]byte       `json:"FontItalic"`
	FontBold     *[]byte       `json:"FontBold"`
	FontSemibold *[]byte       `json:"FontSemibold"`
	// EmbedSource makes the compile response's render options embed the
	// original D2 files and the options set here in the rendered SVG.
	EmbedSource *bool `json:"embedSource"`
}

// EmbedSourceRequest embeds Source in an SVG or PNG image.
type EmbedSourceRequest struct {
	Data   []byte             `json:"data"`
	Source *sourcemeta.Bundle `json:"source"`
}

// ExtractSourceRequest reads the source embedded in an SVG or PNG image.
type ExtractSourceRequest struct {
	Data []byte `json:"data"`
}

// ExtractSourceResponse is ready to pass to compile.
type ExtractSourceResponse struct {
	FS        map[string]string  `json:"fs"`
	InputPath string             `json:"inputPath"`
	Options   ExtractedOptions   `json:"options"`
	Source    *sourcemeta.Bundle `json:"source"`
}

// ExtractedOptions are the compile options an embedded source records. Only
// the set ones are serialized.
type ExtractedOptions struct {
	Layout          *string `json:"layout,omitempty"`
	ThemeID         *int64  `json:"themeID,omitempty"`
	DarkThemeID     *int64  `json:"darkThemeID,omitempty"`
	Sketch          *bool   `json:"sketch,omitempty"`
	Pad             *int64  `json:"pad,omitempty"`
	Center          *bool   `json:"center,omitempty"`
	Isometric       *bool   `json:"isometric,omitempty"`
	AnimateInterval *int64  `json:"animateInterval,omitempty"`
	Target          *string `json:"target,omitempty"`
	EmbedSource     *bool   `json:"embedSource,omitempty"`
}

type CompileResponse struct {
	FS            map[string]string `json:"fs"`
	InputPath     string            `json:"inputPath"`
	Diagram       d2target.Diagram  `json:"diagram"`
	Graph         d2graph.Graph     `json:"graph"`
	RenderOptions RenderOptions     `json:"renderOptions"`
}

type CompletionResponse struct {
	Items []map[string]interface{} `json:"items"`
}

type RenderRequest struct {
	Diagram *d2target.Diagram `json:"diagram"`
	Opts    *RenderOptions    `json:"options"`
}

// RouteEdgesRequest asks TALA to route connections between shapes whose final
// geometry the caller has already decided.
type RouteEdgesRequest struct {
	FS        map[string]string `json:"fs"`
	InputPath *string           `json:"inputPath"`
	// Opts carries what changes measurements (fonts, theme, sketch) and the
	// layout engine whose default label positions apply.
	Opts *CompileOptions `json:"options"`
	// Shapes is the final geometry of every shape, in absolute diagram
	// coordinates.
	Shapes []RouteShape `json:"shapes"`
	// Edges lists the ids of the connections to route.
	Edges []string `json:"edges"`
	// Connections optionally carries the current routes of other connections.
	// TALA keeps them and spreads the new routes and labels around them.
	Connections []RouteConnection `json:"connections"`
	// TimeoutMs bounds the whole call [default: 1500].
	TimeoutMs *float64 `json:"timeoutMs"`
}

type RouteShape struct {
	ID string `json:"id"`
	// Pointers tell a missing or null (NaN in JavaScript) number from zero.
	X             *float64 `json:"x"`
	Y             *float64 `json:"y"`
	Width         *float64 `json:"width"`
	Height        *float64 `json:"height"`
	LabelPosition *string  `json:"labelPosition"`
	IconPosition  *string  `json:"iconPosition"`
}

type RouteConnection struct {
	ID              string             `json:"id"`
	Route           []*RouteInputPoint `json:"route"`
	LabelPosition   *string            `json:"labelPosition"`
	LabelPercentage *float64           `json:"labelPercentage"`
}

type RouteInputPoint struct {
	X *float64 `json:"x"`
	Y *float64 `json:"y"`
}

type RoutePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type RouteLabel struct {
	Position   string  `json:"position"`
	Percentage float64 `json:"percentage"`
}

type RouteEdgesResponse struct {
	// Routes maps each routed connection id to its new polyline, from the
	// source shape's outline to the destination's.
	Routes map[string][]RoutePoint `json:"routes"`
	// Labels holds the label placement TALA picked for routed connections
	// that have a label.
	Labels map[string]RouteLabel `json:"labels,omitempty"`
	// Errors explains each requested connection that got no route.
	Errors map[string]string `json:"errors,omitempty"`
}
