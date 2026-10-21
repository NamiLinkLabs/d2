// Package layoutdata describes the layout-engine options a diagram can set
// under vars.d2-config.data, and reads them back from d2graph.Graph.Data.
//
// Each key is the name of the matching command-line flag without its leading
// dashes: --elk-nodeNode is set in a diagram as
//
//	vars: {
//	  d2-config: {
//	    data: {
//	      elk-nodeNode: 30
//	    }
//	  }
//	}
//
// A value set in the diagram takes precedence over the flag and over options
// passed by D2.js or Go callers, so a diagram lays out the same everywhere.
//
// The package is a leaf: the compiler uses it to validate values with source
// positions and the layout engines use it to read them.
package layoutdata

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Keys of the layout options a diagram can set.
const (
	ELKAlgorithm             = "elk-algorithm"
	ELKNodeNodeBetweenLayers = "elk-nodeNodeBetweenLayers"
	ELKPadding               = "elk-padding"
	ELKEdgeNodeBetweenLayers = "elk-edgeNodeBetweenLayers"
	ELKEdgeEdgeBetweenLayers = "elk-edgeEdgeBetweenLayers"
	ELKNodeSelfLoop          = "elk-nodeSelfLoop"
	ELKNodeNode              = "elk-nodeNode"
	ELKEdgeNode              = "elk-edgeNode"
	ELKEdgeEdge              = "elk-edgeEdge"

	DagreNodeSep = "dagre-nodesep"
	DagreEdgeSep = "dagre-edgesep"
	DagreRankSep = "dagre-ranksep"
	DagrePadding = "dagre-padding"

	TALASeeds = "tala-seeds"
)

// MaxPixels bounds the spacing and padding a diagram can ask for. Larger
// values only produce unusably sparse layouts, and diagrams are untrusted
// input in D2.js.
const MaxPixels = 10000

// MaxListEntries bounds list values such as tala-seeds before they are parsed.
const MaxListEntries = 64

// Kind is the type of value a key takes.
type Kind int

const (
	// Pixels is a whole number from 0 to MaxPixels.
	Pixels Kind = iota
	// Padding is container padding: one number for all four sides, or ELK's
	// "[top=T,left=L,bottom=B,right=R]" where sides left out are 0.
	Padding
	// ELKAlgorithmID is the id of an ELK layout algorithm.
	ELKAlgorithmID
	// Int64List is a list of signed 64-bit integers.
	Int64List
)

// Spec describes one key.
type Spec struct {
	Key    string
	Engine string
	Kind   Kind
}

var specs = []Spec{
	{Key: ELKAlgorithm, Engine: "elk", Kind: ELKAlgorithmID},
	{Key: ELKNodeNodeBetweenLayers, Engine: "elk", Kind: Pixels},
	{Key: ELKPadding, Engine: "elk", Kind: Padding},
	{Key: ELKEdgeNodeBetweenLayers, Engine: "elk", Kind: Pixels},
	{Key: ELKEdgeEdgeBetweenLayers, Engine: "elk", Kind: Pixels},
	{Key: ELKNodeSelfLoop, Engine: "elk", Kind: Pixels},
	{Key: ELKNodeNode, Engine: "elk", Kind: Pixels},
	{Key: ELKEdgeNode, Engine: "elk", Kind: Pixels},
	{Key: ELKEdgeEdge, Engine: "elk", Kind: Pixels},

	{Key: DagreNodeSep, Engine: "dagre", Kind: Pixels},
	{Key: DagreEdgeSep, Engine: "dagre", Kind: Pixels},
	{Key: DagreRankSep, Engine: "dagre", Kind: Pixels},
	{Key: DagrePadding, Engine: "dagre", Kind: Padding},

	{Key: TALASeeds, Engine: "tala", Kind: Int64List},
}

// elkAlgorithms are the algorithms elk-go registers, by their short names.
// ELK also accepts them prefixed with "org.eclipse.elk.".
var elkAlgorithms = []string{
	"layered", "mrtree", "radial", "force", "stress", "rectpacking",
	"sporeOverlap", "sporeCompaction", "box", "fixed", "random",
}

// engineNames are the engines that own a key namespace, with the name used in
// messages. Unknown keys under a strict namespace are errors; tala-* stays
// open so TALA options can be added without breaking diagrams that set them.
var engineNames = map[string]struct {
	name   string
	strict bool
}{
	"elk":   {name: "ELK", strict: true},
	"dagre": {name: "dagre", strict: true},
	"tala":  {name: "TALA", strict: false},
}

// Specs returns every key, in a stable order.
func Specs() []Spec {
	return append([]Spec(nil), specs...)
}

// Lookup returns the spec of key. D2 matches field names case-insensitively,
// so Lookup does too.
func Lookup(key string) (Spec, bool) {
	for _, spec := range specs {
		if strings.EqualFold(spec.Key, key) {
			return spec, true
		}
	}
	return Spec{}, false
}

// Engine returns the layout engine whose namespace key is in ("elk", "dagre"
// or "tala"), or "" when key belongs to no engine.
func Engine(key string) string {
	prefix, _, ok := strings.Cut(key, "-")
	if !ok {
		return ""
	}
	prefix = strings.ToLower(prefix)
	if _, ok := engineNames[prefix]; ok {
		return prefix
	}
	return ""
}

// CheckKey returns an error when key is in the elk-* or dagre-* namespace but
// is not a known option. Other keys, including tala-*, are free-form.
func CheckKey(key string) error {
	if _, ok := Lookup(key); ok {
		return nil
	}
	engine := Engine(key)
	if engine == "" || !engineNames[engine].strict {
		return nil
	}
	msg := fmt.Sprintf("%q is not a valid %s option", key, engineNames[engine].name)
	if suggestion := suggest(key, engine); suggestion != "" {
		msg += fmt.Sprintf(`, did you mean %q?`, suggestion)
	}
	return fmt.Errorf("%s", msg)
}

// Validate checks value against spec. value has the shape the compiler gives
// config data: a string for a scalar and a []interface{} of strings for an
// array. Go callers may also pass numbers and other slices.
func Validate(spec Spec, value any) error {
	var err error
	switch spec.Kind {
	case Pixels:
		_, err = pixels(spec.Key, value)
	case Padding:
		_, err = padding(spec.Key, value)
	case ELKAlgorithmID:
		_, err = algorithm(spec.Key, value)
	case Int64List:
		_, err = int64List(spec.Key, value)
	}
	return err
}

// Value returns the value data holds under key, matching key
// case-insensitively. An exact match wins over a case-insensitive one.
func Value(data map[string]any, key string) (any, bool) {
	if v, ok := data[key]; ok {
		return v, true
	}
	var matches []string
	for k := range data {
		if strings.EqualFold(k, key) {
			matches = append(matches, k)
		}
	}
	if len(matches) == 0 {
		return nil, false
	}
	sort.Strings(matches)
	return data[matches[0]], true
}

// Int returns the number of pixels data sets under key.
func Int(data map[string]any, key string) (int, bool, error) {
	v, ok := Value(data, key)
	if !ok {
		return 0, false, nil
	}
	n, err := pixels(key, v)
	return n, err == nil, err
}

// PaddingValue returns the padding data sets under key in ELK syntax,
// "[top=T,left=L,bottom=B,right=R]".
func PaddingValue(data map[string]any, key string) (string, bool, error) {
	v, ok := Value(data, key)
	if !ok {
		return "", false, nil
	}
	p, err := padding(key, v)
	if err != nil {
		return "", false, err
	}
	return p.String(), true, nil
}

// Algorithm returns the ELK algorithm data sets under key.
func Algorithm(data map[string]any, key string) (string, bool, error) {
	v, ok := Value(data, key)
	if !ok {
		return "", false, nil
	}
	s, err := algorithm(key, v)
	return s, err == nil, err
}

// Int64s returns the list of integers data sets under key.
func Int64s(data map[string]any, key string) ([]int64, bool, error) {
	v, ok := Value(data, key)
	if !ok {
		return nil, false, nil
	}
	list, err := int64List(key, v)
	return list, err == nil, err
}

// Signature returns the layout options data sets for engine, in a canonical
// form, so callers caching layouts can tell inputs apart. Values that do not
// parse are included as written, since layout will reject them anyway.
func Signature(data map[string]any, engine string) string {
	var b strings.Builder
	for _, spec := range specs {
		if spec.Engine != engine {
			continue
		}
		v, ok := Value(data, spec.Key)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s=%s;", spec.Key, describe(v))
	}
	return b.String()
}

// Sides is container padding in pixels.
type Sides struct {
	Top, Left, Bottom, Right int
}

// String formats p in ELK syntax.
func (p Sides) String() string {
	return fmt.Sprintf("[top=%d,left=%d,bottom=%d,right=%d]", p.Top, p.Left, p.Bottom, p.Right)
}

// ParsePadding parses one number for all four sides, or ELK's
// "[top=T,left=L,bottom=B,right=R]" where sides left out are 0, as in ELK.
// Brackets are optional and whitespace is ignored.
func ParsePadding(s string) (Sides, error) {
	s = strings.TrimSpace(s)
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		n, err := parsePixels(s)
		if err != nil {
			return Sides{}, err
		}
		return Sides{n, n, n, n}, nil
	}
	inner := s
	if strings.HasPrefix(inner, "[") && strings.HasSuffix(inner, "]") {
		inner = inner[1 : len(inner)-1]
	}
	if strings.TrimSpace(inner) == "" {
		return Sides{}, fmt.Errorf("no sides given")
	}
	var p Sides
	seen := map[string]bool{}
	for _, part := range strings.Split(inner, ",") {
		name, raw, ok := strings.Cut(part, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		if !ok {
			return Sides{}, fmt.Errorf("%q is not side=pixels", strings.TrimSpace(part))
		}
		n, err := parsePixels(strings.TrimSpace(raw))
		if err != nil {
			return Sides{}, fmt.Errorf("%s: %v", name, err)
		}
		if seen[name] {
			return Sides{}, fmt.Errorf("%s is given twice", name)
		}
		seen[name] = true
		switch name {
		case "top":
			p.Top = n
		case "left":
			p.Left = n
		case "bottom":
			p.Bottom = n
		case "right":
			p.Right = n
		default:
			return Sides{}, fmt.Errorf("%q is not a side (top, left, bottom or right)", name)
		}
	}
	return p, nil
}

func pixels(key string, v any) (int, error) {
	n, err := toPixels(v)
	if err != nil {
		return 0, fmt.Errorf("expected a whole number of pixels from 0 to %d for %q, got %q", MaxPixels, key, describe(v))
	}
	return n, nil
}

func padding(key string, v any) (Sides, error) {
	if n, err := toPixels(v); err == nil {
		return Sides{n, n, n, n}, nil
	}
	s, ok := v.(string)
	if !ok {
		return Sides{}, fmt.Errorf(`expected a number or "[top=..,left=..,bottom=..,right=..]" for %q, got %q`, key, describe(v))
	}
	p, err := ParsePadding(s)
	if err != nil {
		return Sides{}, fmt.Errorf(`expected a number or "[top=..,left=..,bottom=..,right=..]" for %q, got %q: %v`, key, s, err)
	}
	return p, nil
}

func algorithm(key string, v any) (string, error) {
	s, ok := v.(string)
	if ok {
		name := strings.TrimPrefix(s, "org.eclipse.elk.")
		for _, known := range elkAlgorithms {
			if name == known {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("expected one of %s for %q, got %q", strings.Join(elkAlgorithms, ", "), key, describe(v))
}

func int64List(key string, v any) ([]int64, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice) {
		return nil, fmt.Errorf("expected a list of signed 64-bit integers for %q, got %q", key, describe(v))
	}
	if rv.Len() > MaxListEntries {
		return nil, fmt.Errorf("%q accepts at most %d entries, got %d", key, MaxListEntries, rv.Len())
	}
	list := make([]int64, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i).Interface()
		n, err := toInt64(item)
		if err != nil {
			return nil, fmt.Errorf("invalid %q entry at index %d (%s): must be a signed 64-bit integer", key, i, describe(item))
		}
		list = append(list, n)
	}
	return list, nil
}

func toPixels(v any) (int, error) {
	var n int64
	switch v := v.(type) {
	case string:
		p, err := parsePixels(strings.TrimSpace(v))
		return p, err
	case int:
		n = int64(v)
	case int64:
		n = v
	case int32:
		n = int64(v)
	case float64:
		if v != math.Trunc(v) || math.IsInf(v, 0) || v < 0 || v > MaxPixels {
			return 0, fmt.Errorf("out of range")
		}
		n = int64(v)
	case fmt.Stringer:
		return parsePixels(strings.TrimSpace(v.String()))
	default:
		return 0, fmt.Errorf("not a number")
	}
	if n < 0 || n > MaxPixels {
		return 0, fmt.Errorf("out of range")
	}
	return int(n), nil
}

func parsePixels(s string) (int, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a whole number", s)
	}
	if n < 0 || n > MaxPixels {
		return 0, fmt.Errorf("%d is not between 0 and %d", n, MaxPixels)
	}
	return int(n), nil
}

func toInt64(v any) (int64, error) {
	switch v := v.(type) {
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case int32:
		return int64(v), nil
	case float64:
		if v != math.Trunc(v) || v < math.MinInt64 || v >= math.MaxInt64 {
			return 0, fmt.Errorf("not an integer")
		}
		return int64(v), nil
	}
	return strconv.ParseInt(fmt.Sprint(v), 10, 64)
}

// describe formats a value for an error message the way it was written.
func describe(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case string:
		return v
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		parts := make([]string, rv.Len())
		for i := range parts {
			parts[i] = describe(rv.Index(i).Interface())
		}
		return "[" + strings.Join(parts, "; ") + "]"
	case reflect.Map:
		return "{...}"
	}
	return fmt.Sprint(v)
}

// suggest returns the known key of engine closest to key, if it is close
// enough to be a likely typo.
func suggest(key, engine string) string {
	best, bestDist := "", math.MaxInt
	lower := strings.ToLower(key)
	for _, spec := range specs {
		if spec.Engine != engine {
			continue
		}
		d := editDistance(lower, strings.ToLower(spec.Key))
		if d < bestDist {
			best, bestDist = spec.Key, d
		}
	}
	// Allow roughly one edit in three characters of the option's own name.
	if bestDist <= max(2, (len(best)-len(engine)-1)/3) {
		return best
	}
	return ""
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
