package d2talalayout

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
	"github.com/d2lang/d2/lib/layoutdata"
)

const (
	maxSeeds                  = 16
	maxSeedEntries            = maxSeeds * 4
	defaultMaxSeedConcurrency = 4
)

// Options controls deterministic local layout attempts. The zero value uses
// the package defaults. Diagram data stored on the D2 graph takes precedence
// over Options: tala-seeds over Seeds, tala-quality over Quality and
// tala-separate-edges over SeparateEdges. MaxConcurrency bounds simultaneous
// attempts; zero selects the package default.
type Options struct {
	// Seeds selects deterministic attempts. Nil uses the package defaults; an
	// explicitly empty slice is invalid. QualityFast runs only the first seed.
	Seeds []int64 `json:"tala-seeds"`

	// Quality trades layout quality for speed. Empty selects QualityFull.
	Quality Quality `json:"tala-quality"`

	// SeparateEdges gives every connection its own route. By default TALA
	// bundles connections that share an endpoint: they leave or enter it
	// through one port and share a trunk, tree edges form a bus, and fans
	// into a cluster of shapes merge. With SeparateEdges, routes avoid
	// sharing segments and ports with each other. This is a strong
	// preference, not a guarantee: a crowded side may still force an
	// overlap. False keeps the default, bundled routes.
	SeparateEdges bool `json:"tala-separate-edges"`

	// MaxConcurrency bounds simultaneous attempts. Zero uses the package default.
	MaxConcurrency int `json:"-"`
}

// DefaultOptions returns an independent copy of the package defaults.
func DefaultOptions() Options {
	return Options{
		Seeds:          defaultSeeds(),
		MaxConcurrency: defaultSeedConcurrency(),
	}
}

// DefaultLayout lays out graph with fresh default options. Like all D2 layout
// functions, it mutates graph and must not run concurrently against the same
// graph.
func DefaultLayout(ctx context.Context, graph *d2graph.Graph) error {
	return Layout(ctx, graph, nil)
}

type localSeedAttempt struct {
	index  int
	seed   int64
	result seedResult
	err    error
}

func defaultSeedConcurrency() int {
	return min(runtime.GOMAXPROCS(0), defaultMaxSeedConcurrency)
}

func normalizeSeeds(seeds []int64) ([]int64, error) {
	if len(seeds) == 0 {
		return nil, fmt.Errorf("tala requires at least one seed")
	}
	if len(seeds) > maxSeedEntries {
		return nil, fmt.Errorf("tala accepts at most %d seed entries", maxSeedEntries)
	}

	capacity := min(len(seeds), maxSeeds)
	unique := make([]int64, 0, capacity)
	seen := make(map[int64]struct{}, capacity)
	for _, seed := range seeds {
		if _, ok := seen[seed]; ok {
			continue
		}
		seen[seed] = struct{}{}
		unique = append(unique, seed)
		if len(unique) > maxSeeds {
			return nil, fmt.Errorf("tala supports at most %d unique seeds", maxSeeds)
		}
	}
	return unique, nil
}

// layoutRequest is the resolved, validated plan of one Layout call.
type layoutRequest struct {
	seeds       []int64
	concurrency int
	quality     Quality
	profile     qualityProfile
	// separateEdges gives every edge its own route.
	separateEdges bool
}

// layoutPlan resolves options and diagram data into a layout request. Diagram
// data takes precedence over options, and options over package defaults, for
// both the seeds and the quality level. Seeds are validated in full before the
// quality level keeps only the ones it runs.
func layoutPlan(graph *d2graph.Graph, opts *Options) (layoutRequest, error) {
	defaults := DefaultOptions()
	if opts == nil {
		opts = &defaults
	}

	quality := opts.Quality
	if graph.Data != nil {
		if raw, ok := layoutdata.Value(graph.Data, layoutdata.TALAQuality); ok {
			var err error
			quality, err = qualityFromData(raw)
			if err != nil {
				return layoutRequest{}, err
			}
		}
	}
	profile, err := quality.profile()
	if err != nil {
		return layoutRequest{}, err
	}
	if quality == "" {
		quality = QualityFull
	}

	seeds := opts.Seeds
	if seeds == nil {
		seeds = defaults.Seeds
	}
	if graph.Data != nil {
		dataSeeds, ok, err := layoutdata.Int64s(graph.Data, layoutdata.TALASeeds)
		if err != nil {
			return layoutRequest{}, err
		}
		if ok {
			seeds = dataSeeds
		}
	}

	seeds, err = normalizeSeeds(seeds)
	if err != nil {
		return layoutRequest{}, err
	}
	seeds = profile.limitSeeds(seeds)
	concurrency := opts.MaxConcurrency
	if concurrency == 0 {
		concurrency = defaults.MaxConcurrency
	}
	if concurrency < 0 || concurrency > maxSeeds {
		return layoutRequest{}, fmt.Errorf("tala MaxConcurrency must be between 1 and %d, or zero for the default", maxSeeds)
	}
	if concurrency > len(seeds) {
		concurrency = len(seeds)
	}
	separateEdges, err := resolveSeparateEdges(graph, opts)
	if err != nil {
		return layoutRequest{}, err
	}
	return layoutRequest{seeds: seeds, concurrency: concurrency, quality: quality, profile: profile, separateEdges: separateEdges}, nil
}

func runLocalSeed(
	ctx context.Context,
	input seedInput,
	index int,
	seed int64,
) (attempt localSeedAttempt) {
	attempt.index = index
	attempt.seed = seed
	defer func() {
		if recover() != nil {
			attempt.result = seedResult{}
			attempt.err = fmt.Errorf("TALA seed layout failed due to an internal invariant")
		}
	}()

	laidOut, err := runSeed(ctx, input, seed)
	if err != nil {
		attempt.err = err
		return attempt
	}
	attempt.result, attempt.err = evaluateSeedResult(ctx, input, laidOut)
	return attempt
}

type localSeedFunc func(index int, seed int64) localSeedAttempt

func coordinateLocalSeeds(ctx context.Context, seeds []int64, concurrency int, run localSeedFunc) (seedResult, error) {
	if len(seeds) == 0 {
		return seedResult{}, fmt.Errorf("tala requires at least one seed")
	}
	if concurrency < 1 {
		return seedResult{}, fmt.Errorf("tala seed concurrency must be positive")
	}
	if concurrency > len(seeds) {
		concurrency = len(seeds)
	}
	jobs := make(chan int, len(seeds))
	for i := range seeds {
		jobs <- i
	}
	close(jobs)

	results := make(chan localSeedAttempt)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() {
			for i := range jobs {
				results <- run(i, seeds[i])
			}
		})
	}
	go func() {
		workers.Wait()
		close(results)
	}()

	bestIndex := -1
	var best seedResult
	attemptErrors := make([]error, len(seeds))
	for attempt := range results {
		if attempt.err != nil {
			attemptErrors[attempt.index] = fmt.Errorf("seed %d: %w", attempt.seed, attempt.err)
			continue
		}
		comparison := 0
		if bestIndex >= 0 {
			comparison = attempt.result.score.compare(best.score)
		}
		if bestIndex < 0 || comparison < 0 || (comparison == 0 && attempt.index > bestIndex) {
			bestIndex = attempt.index
			best = attempt.result
		}
	}

	if err := ctx.Err(); err != nil {
		return seedResult{}, fmt.Errorf("tala layout stopped before all seeds completed: %w", err)
	}

	if bestIndex < 0 {
		joined := errors.Join(attemptErrors...)
		if joined == nil {
			return seedResult{}, fmt.Errorf("no TALA seed produced a layout")
		}
		return seedResult{}, fmt.Errorf("all TALA seed attempts failed: %w", joined)
	}
	return best, nil
}

func runLocalSeeds(ctx context.Context, input seedInput, seeds []int64, concurrency int) (seedResult, error) {
	return coordinateLocalSeeds(ctx, seeds, concurrency, func(index int, seed int64) localSeedAttempt {
		return runLocalSeed(ctx, input, index, seed)
	})
}

// Layout runs every configured seed locally, selects the best complete result
// deterministically, and then atomically applies it to graph. It never selects
// a result from only a timing-dependent subset of the configured seeds. The
// caller must not mutate graph concurrently.
func Layout(ctx context.Context, graph *d2graph.Graph, opts *Options) (err error) {
	defer recoverAsError("layout", &err)

	if ctx == nil {
		return fmt.Errorf("TALA layout requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if graph == nil || graph.Root == nil {
		return fmt.Errorf("tala requires a D2 graph with a root object")
	}
	request, err := layoutPlan(graph, opts)
	if err != nil {
		return err
	}
	input, err := newSeedInput(ctx, graph)
	if err != nil {
		return err
	}
	input.effort = request.profile.effort
	input.graph.SeparateEdges = request.separateEdges
	// Logging and request metadata commonly wrap Background with values, leaving
	// Done nil. Give the CPU-heavy engine phase a real cancellation channel so
	// its hot work guards can poll that channel without repeatedly traversing
	// every value wrapper. Values and parent cancellation semantics are retained.
	workCtx := ctx
	cancelWork := func() {}
	if ctx.Done() == nil {
		workCtx, cancelWork = context.WithCancel(ctx)
	}
	defer cancelWork()
	pins := newAbsolutePins(input.graph)
	best, err := layoutSeedInput(workCtx, input, request.seeds, request.concurrency)
	if err != nil {
		return err
	}
	// Nested pins use an estimated container padding first. When the selected
	// layout shows a different padding, one more pass places them exactly.
	if pins.correct(workCtx, best.graph) {
		best, err = layoutSeedInput(workCtx, input, request.seeds, request.concurrency)
		if err != nil {
			return err
		}
	}
	return applySeedResult(workCtx, graph, best)
}

// layoutSeedInput runs every seed and the optional compound strategies on
// input and returns the selected result without applying it.
func layoutSeedInput(workCtx context.Context, input seedInput, seeds []int64, concurrency int) (seedResult, error) {
	best, err := runLocalSeeds(workCtx, input, seeds, concurrency)
	if err != nil {
		return seedResult{}, err
	}
	ordinary := best
	best, err = considerSeedCandidate(workCtx, best, func() (seedResult, error) {
		candidate, err := engine.CompoundCandidateWithEffort(workCtx, best.graph, input.effort)
		if err != nil {
			return seedResult{}, err
		}
		if candidate == best.graph {
			return best, nil
		}
		return evaluateSeedResult(workCtx, input, candidate)
	})
	if err != nil {
		return seedResult{}, err
	}
	if best.graph != ordinary.graph {
		selected := best
		best, err = refineSeedResult(workCtx, selected, func() (seedResult, error) {
			candidate, err := engine.PreserveCompoundRoutes(workCtx, ordinary.graph, selected.graph)
			if err != nil {
				return seedResult{}, err
			}
			return evaluateSeedResult(workCtx, input, candidate)
		})
		if err != nil {
			return seedResult{}, err
		}
	}
	return best, nil
}

// Optional strategies may reject a geometry or exhaust their own search. They
// must not invalidate a complete ordinary result. Caller cancellation remains
// an error; a timing-dependent subset of candidates is never selected.
func considerSeedCandidate(ctx context.Context, incumbent seedResult, build func() (seedResult, error)) (seedResult, error) {
	candidate, err := refineSeedResult(ctx, incumbent, build)
	if err != nil {
		return incumbent, err
	}
	if candidate.score.compare(incumbent.score) < 0 {
		return candidate, nil
	}
	return incumbent, nil
}

// A routing refinement preserves the selected placement. Its enclosure checks
// retain already-valid interior paths; the completed-layout score does
// not measure their shared fan structure. Validation still runs before apply.
func refineSeedResult(ctx context.Context, incumbent seedResult, build func() (seedResult, error)) (selected seedResult, err error) {
	selected = incumbent
	defer func() {
		if recover() != nil {
			selected, err = incumbent, nil
		}
		if contextErr := ctx.Err(); contextErr != nil {
			err = contextErr
		}
	}()
	candidate, candidateErr := build()
	if candidateErr != nil {
		if errors.Is(candidateErr, context.Canceled) || errors.Is(candidateErr, context.DeadlineExceeded) {
			return incumbent, candidateErr
		}
		return incumbent, nil
	}
	return candidate, nil
}
