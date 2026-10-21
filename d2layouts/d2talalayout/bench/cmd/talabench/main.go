// Command talabench lays out the TALA benchmark corpus with TALA, dagre and
// ELK and prints timing and layout-quality metrics as a Markdown table or
// JSON lines. See d2layouts/d2talalayout/bench/BASELINE.md.
//
//	go run ./d2layouts/d2talalayout/bench/cmd/talabench -timeout 3m
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"time"

	"github.com/d2lang/d2/d2layouts/d2talalayout/bench"
	"github.com/d2lang/d2/d2target"
)

type record struct {
	Case          string  `json:"case"`
	Engine        string  `json:"engine"`
	Objects       int     `json:"objects"`
	Edges         int     `json:"edges"`
	LayoutMS      float64 `json:"layout_ms"`
	CompileMS     float64 `json:"compile_ms"`
	Crossings     int     `json:"crossings"`
	Bends         int     `json:"bends"`
	EdgeLength    float64 `json:"edge_length"`
	SharedLength  float64 `json:"shared_length"`
	BBoxWidth     int     `json:"bbox_width"`
	BBoxHeight    int     `json:"bbox_height"`
	EmptyFraction float64 `json:"empty_fraction"`
	EditLayoutMS  float64 `json:"edit_layout_ms,omitempty"`
	DispCompared  int     `json:"disp_compared,omitempty"`
	DispMean      float64 `json:"disp_mean"`
	DispMax       float64 `json:"disp_max"`
	DispMoved     int     `json:"disp_moved"`
	Error         string  `json:"error,omitempty"`
	EditError     string  `json:"edit_error,omitempty"`
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func main() {
	corpus := flag.String("corpus", "d2layouts/d2talalayout/bench/testdata/corpus", "directory of .d2 corpus files")
	cases := flag.String("cases", ".", "regular expression selecting corpus file stems")
	engines := flag.String("engines", "tala,tala-1seed,dagre,elk", "comma-separated engine configurations")
	timeout := flag.Duration("timeout", 3*time.Minute, "per-layout timeout (0 disables)")
	noEdit := flag.Bool("no-edit", false, "skip the one-line-edit stability measurement")
	format := flag.String("format", "markdown", "output format: markdown or jsonl")
	cpuprofile := flag.String("cpuprofile", "", "write a CPU profile (samples are labeled engine=..., phase=layout|routing)")
	memprofile := flag.String("memprofile", "", "write an allocation profile (go tool pprof -sample_index=alloc_space) after all cases")
	dump := flag.String("dump", "", "write each layout's final geometry (bench.GeometryJSON) to DIR/<case>.<engine>[.edit].json and print its SHA-256 to stderr")
	flag.Parse()

	die := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "talabench:", err)
			os.Exit(1)
		}
	}
	match, err := regexp.Compile(*cases)
	die(err)
	var selected []bench.Engine
	for _, name := range strings.Split(*engines, ",") {
		e, ok := bench.EngineByName(strings.TrimSpace(name))
		if !ok {
			die(fmt.Errorf("unknown engine %q", name))
		}
		selected = append(selected, e)
	}
	files, err := filepath.Glob(filepath.Join(*corpus, "*.d2"))
	die(err)
	slices.Sort(files)
	if len(files) == 0 {
		die(fmt.Errorf("no .d2 files in %s", *corpus))
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		die(err)
		die(pprof.StartCPUProfile(f))
		defer f.Close()
		defer pprof.StopCPUProfile()
	}

	if *memprofile != "" {
		defer func() {
			f, err := os.Create(*memprofile)
			die(err)
			die(pprof.Lookup("allocs").WriteTo(f, 0))
			die(f.Close())
		}()
	}

	fmt.Fprintf(os.Stderr, "talabench: go=%s GOMAXPROCS=%d GOOS/GOARCH=%s/%s timeout=%s\n",
		runtime.Version(), runtime.GOMAXPROCS(0), runtime.GOOS, runtime.GOARCH, *timeout)
	if *format == "markdown" {
		fmt.Println("| case | objects | edges | engine | layout ms | compile ms | crossings | bends | edge length | shared | bbox | empty % | edit layout ms | disp mean | disp max | moved |")
		fmt.Println("|---|---:|---:|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---:|---:|---:|")
	}
	enc := json.NewEncoder(os.Stdout)
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".d2")
		if !match.MatchString(name) {
			continue
		}
		b, err := os.ReadFile(path)
		die(err)
		source := string(b)
		edit, _ := bench.EditFromSource(source)
		if *noEdit {
			edit = ""
		}
		for _, engine := range selected {
			r := bench.MeasureCase(context.Background(), name, source, edit, engine, *timeout)
			rec := record{
				Case: r.Case, Engine: r.Engine, Objects: r.Objects, Edges: r.Edges,
				LayoutMS: ms(r.Layout), CompileMS: ms(r.Compile),
				Crossings: r.Metrics.Crossings, Bends: r.Metrics.Bends, EdgeLength: r.Metrics.EdgeLength, SharedLength: r.Metrics.SharedLength,
				BBoxWidth: r.Metrics.BBoxWidth, BBoxHeight: r.Metrics.BBoxHeight, EmptyFraction: r.Metrics.EmptyFraction,
				EditLayoutMS: ms(r.EditLayout),
			}
			if r.Displacement != nil {
				rec.DispCompared = r.Displacement.Compared
				rec.DispMean, rec.DispMax, rec.DispMoved = r.Displacement.Mean, r.Displacement.Max, r.Displacement.Moved
			}
			if r.Err != nil {
				rec.Error = r.Err.Error()
			}
			if r.EditErr != nil {
				rec.EditError = r.EditErr.Error()
			}
			if *dump != "" {
				die(dumpGeometry(*dump, name+"."+engine.Name, r.Diagram))
				die(dumpGeometry(*dump, name+"."+engine.Name+".edit", r.EditDiagram))
			}
			fmt.Fprintf(os.Stderr, "%s %s layout=%.0fms edit=%.0fms err=%v editErr=%v\n", name, engine.Name, rec.LayoutMS, rec.EditLayoutMS, r.Err, r.EditErr)
			if *format == "jsonl" {
				die(enc.Encode(rec))
				continue
			}
			printRow(rec, r)
		}
	}
}

// dumpGeometry writes diagram's geometry to dir/stem.json and prints its
// SHA-256. A nil diagram (failed or skipped compile) writes nothing.
func dumpGeometry(dir, stem string, diagram *d2target.Diagram) error {
	if diagram == nil {
		return nil
	}
	b, err := bench.GeometryJSON(diagram)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, stem+".json"), b, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "geometry %x %s\n", sha256.Sum256(b), stem)
	return nil
}

func printRow(rec record, r bench.Result) {
	if r.Err != nil {
		status := "error"
		if errors.Is(r.Err, bench.ErrTimeout) {
			status = "timeout"
		}
		fmt.Printf("| %s | | | %s | %s (%.0f) | | | | | | | | | | | |\n", rec.Case, rec.Engine, status, rec.LayoutMS)
		return
	}
	edit, mean, maxd, moved := "", "", "", ""
	switch {
	case r.Displacement != nil:
		edit = fmt.Sprintf("%.0f", rec.EditLayoutMS)
		mean = fmt.Sprintf("%.1f", rec.DispMean)
		maxd = fmt.Sprintf("%.1f", rec.DispMax)
		moved = fmt.Sprintf("%d/%d", rec.DispMoved, rec.DispCompared)
	case r.EditErr != nil && errors.Is(r.EditErr, bench.ErrTimeout):
		edit = "timeout"
	case r.EditErr != nil:
		edit = "error"
	}
	fmt.Printf("| %s | %d | %d | %s | %.0f | %.0f | %d | %d | %.0f | %.0f | %dx%d | %.1f | %s | %s | %s | %s |\n",
		rec.Case, rec.Objects, rec.Edges, rec.Engine, rec.LayoutMS, rec.CompileMS,
		rec.Crossings, rec.Bends, rec.EdgeLength, rec.SharedLength, rec.BBoxWidth, rec.BBoxHeight, 100*rec.EmptyFraction,
		edit, mean, maxd, moved)
}
