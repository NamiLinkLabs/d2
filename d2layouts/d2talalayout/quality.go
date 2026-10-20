package d2talalayout

import (
	"fmt"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
)

// Quality trades layout quality for speed. Every level is deterministic: the
// same graph, options and level always produce the same layout. A level fixes
// the amount of work (seed count, iteration counts and optional passes), never
// a time budget, so it gives the same result on a fast and on a slow machine.
// The empty value selects QualityFull. See bench/QUALITY.md for the measured
// trade-off of each level.
type Quality string

const (
	// QualityFull runs every configured seed with the full pipeline. It is the
	// default and produces the same layouts as TALA without a quality level.
	QualityFull Quality = "full"
	// QualityBalanced runs every configured seed with the reduced pipeline of
	// QualityFast: best-of-seeds selection at a fraction of the full cost.
	QualityBalanced Quality = "balanced"
	// QualityFast runs only the first configured seed with a reduced pipeline:
	// a third of the node-placement iterations, one edge-routing order instead
	// of three, and no SwapStuff or GapNormalization passes. It is meant for
	// interactive previews, such as while a user drags a shape.
	QualityFast Quality = "fast"
)

// qualityDataKey is the diagram data key that requests a quality level, e.g.
// vars: {d2-config: {data: {tala-quality: fast}}}.
const qualityDataKey = "tala-quality"

// qualityProfile is the deterministic work plan of one quality level.
type qualityProfile struct {
	// maxSeeds bounds the attempts to the first maxSeeds configured seeds, in
	// first-seen order. Zero runs every configured seed.
	maxSeeds int
	// effort trims the engine pipeline of every seed and of the
	// compound-container refinement.
	effort engine.Effort
}

func defaultSeeds() []int64 {
	return []int64{1, 2, 3}
}

// limitSeeds returns the seeds the profile runs out of the normalized seeds.
func (p qualityProfile) limitSeeds(seeds []int64) []int64 {
	if p.maxSeeds > 0 && len(seeds) > p.maxSeeds {
		return seeds[:p.maxSeeds]
	}
	return seeds
}

// reducedEffort is the per-seed pipeline of QualityBalanced and QualityFast,
// chosen by measurement on the benchmark corpus (bench/QUALITY.md).
func reducedEffort() engine.Effort {
	return engine.Effort{
		PlacementIterations:  30,
		SingleRouteFlavor:    true,
		SkipSwap:             true,
		SkipGapNormalization: true,
	}
}

// profile returns q's work plan. It returns an error for an unknown level.
func (q Quality) profile() (qualityProfile, error) {
	switch q {
	case "", QualityFull:
		return qualityProfile{}, nil
	case QualityBalanced:
		return qualityProfile{effort: reducedEffort()}, nil
	case QualityFast:
		return qualityProfile{maxSeeds: 1, effort: reducedEffort()}, nil
	}
	return qualityProfile{}, fmt.Errorf("unknown tala quality %q: want %q, %q or %q", string(q), QualityFull, QualityBalanced, QualityFast)
}

// qualityFromData reads a quality level from diagram data. D2 stores scalar
// config data as strings.
func qualityFromData(raw any) (Quality, error) {
	value, ok := raw.(string)
	if !ok || value == "" {
		return "", fmt.Errorf("%s must be one of %q, %q or %q", qualityDataKey, QualityFull, QualityBalanced, QualityFast)
	}
	q := Quality(value)
	if _, err := q.profile(); err != nil {
		return "", err
	}
	return q, nil
}
