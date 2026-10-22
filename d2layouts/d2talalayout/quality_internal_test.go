package d2talalayout

import (
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
	"github.com/d2lang/d2/lib/layoutdata"
)

// The compiler validates tala-quality with layoutdata, which keeps its own
// list of the levels; it must accept exactly the levels TALA does.
func TestLayoutDataAcceptsTALAQualities(t *testing.T) {
	spec, ok := layoutdata.Lookup(layoutdata.TALAQuality)
	if !ok {
		t.Fatal("layoutdata does not know tala-quality")
	}
	for _, q := range []Quality{QualityFull, QualityBalanced, QualityFast} {
		if err := layoutdata.Validate(spec, string(q)); err != nil {
			t.Errorf("layoutdata rejects TALA quality %q: %v", q, err)
		}
	}
	for _, q := range []string{"turbo", "Full", ""} {
		_, tala := qualityFromData(q)
		if (layoutdata.Validate(spec, q) == nil) != (tala == nil) {
			t.Errorf("layoutdata and TALA disagree on quality %q", q)
		}
	}
}

func TestQualityProfiles(t *testing.T) {
	full, err := QualityFull.profile()
	if err != nil {
		t.Fatal(err)
	}
	empty, err := Quality("").profile()
	if err != nil {
		t.Fatal(err)
	}
	// Full must stay today's pipeline: every seed, zero effort.
	if full != (qualityProfile{}) || empty != full {
		t.Fatalf("full profile = %+v, empty profile = %+v, want the zero profile", full, empty)
	}
	balanced, err := QualityBalanced.profile()
	if err != nil {
		t.Fatal(err)
	}
	fast, err := QualityFast.profile()
	if err != nil {
		t.Fatal(err)
	}
	if balanced.maxSeeds != 0 || fast.maxSeeds != 1 {
		t.Fatalf("seed bounds: balanced %d, fast %d", balanced.maxSeeds, fast.maxSeeds)
	}
	if balanced.effort == (engine.Effort{}) || fast.effort == (engine.Effort{}) {
		t.Fatal("reduced levels must trim the pipeline")
	}
	for _, q := range []Quality{"Fast", "medium", " fast"} {
		if _, err := q.profile(); err == nil || !strings.Contains(err.Error(), "unknown tala quality") {
			t.Fatalf("quality %q error = %v", q, err)
		}
	}
}

func TestLayoutPlanQualityPrecedence(t *testing.T) {
	tests := []struct {
		name      string
		data      map[string]any
		opts      *Options
		quality   Quality
		seeds     []int64
		effortSet bool
	}{
		{name: "nil options", opts: nil, quality: QualityFull, seeds: []int64{1, 2, 3}},
		{name: "zero options", opts: &Options{}, quality: QualityFull, seeds: []int64{1, 2, 3}},
		{name: "explicit full", opts: &Options{Quality: QualityFull}, quality: QualityFull, seeds: []int64{1, 2, 3}},
		{name: "option balanced", opts: &Options{Quality: QualityBalanced}, quality: QualityBalanced, seeds: []int64{1, 2, 3}, effortSet: true},
		{name: "option fast", opts: &Options{Quality: QualityFast}, quality: QualityFast, seeds: []int64{1}, effortSet: true},
		{name: "fast keeps first configured seed", opts: &Options{Quality: QualityFast, Seeds: []int64{9, 9, 8}}, quality: QualityFast, seeds: []int64{9}, effortSet: true},
		{
			name:    "data overrides option",
			data:    map[string]any{"tala-quality": "full"},
			opts:    &Options{Quality: QualityFast},
			quality: QualityFull, seeds: []int64{1, 2, 3},
		},
		{
			name:    "data fast with default options",
			data:    map[string]any{"tala-quality": "fast"},
			opts:    func() *Options { o := DefaultOptions(); return &o }(),
			quality: QualityFast, seeds: []int64{1}, effortSet: true,
		},
		{
			name:    "data seeds and data quality",
			data:    map[string]any{"tala-quality": "fast", "tala-seeds": []any{"7", "11"}},
			opts:    &Options{Seeds: []int64{4}},
			quality: QualityFast, seeds: []int64{7}, effortSet: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := layoutPlan(&d2graph.Graph{Data: tt.data}, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got.quality != tt.quality || !slices.Equal(got.seeds, tt.seeds) {
				t.Fatalf("plan = (%q, %v), want (%q, %v)", got.quality, got.seeds, tt.quality, tt.seeds)
			}
			if (got.profile.effort != engine.Effort{}) != tt.effortSet {
				t.Fatalf("effort = %+v, want trimmed=%t", got.profile.effort, tt.effortSet)
			}
			if got.concurrency < 1 || got.concurrency > len(got.seeds) {
				t.Fatalf("concurrency = %d for %d seeds", got.concurrency, len(got.seeds))
			}
		})
	}
}

func TestLayoutPlanRejectsInvalidQuality(t *testing.T) {
	tooManySeeds := make([]int64, maxSeeds+1)
	for i := range tooManySeeds {
		tooManySeeds[i] = int64(i)
	}
	tests := []struct {
		name string
		data map[string]any
		opts *Options
		want string
	}{
		{name: "unknown option", opts: &Options{Quality: "turbo"}, want: `unknown tala quality "turbo"`},
		{name: "unknown data", data: map[string]any{"tala-quality": "turbo"}, opts: &Options{}, want: `unknown tala quality "turbo"`},
		{name: "empty data", data: map[string]any{"tala-quality": ""}, opts: &Options{}, want: "tala-quality must be one of"},
		{name: "list data", data: map[string]any{"tala-quality": []any{"fast"}}, opts: &Options{}, want: "tala-quality must be one of"},
		{name: "invalid data wins over valid option", data: map[string]any{"tala-quality": "turbo"}, opts: &Options{Quality: QualityFast}, want: "unknown tala quality"},
		// Every configured seed is validated even when the level runs only one.
		{name: "fast still validates every seed", opts: &Options{Quality: QualityFast, Seeds: tooManySeeds}, want: "at most 16"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := layoutPlan(&d2graph.Graph{Data: tt.data}, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
