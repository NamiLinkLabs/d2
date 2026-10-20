package bench

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func readCorpus(tb testing.TB, name string) (source, edit string) {
	tb.Helper()
	b, err := os.ReadFile(corpusPath(name))
	if err != nil {
		tb.Fatal(err)
	}
	edit, ok := EditFromSource(string(b))
	if !ok {
		tb.Fatalf("%s has no %q line", name, EditDirective)
	}
	return string(b), edit
}

// TestMeasureSmallCase runs the whole harness on the smallest corpus case.
func TestMeasureSmallCase(t *testing.T) {
	source, edit := readCorpus(t, "arch_010")
	for _, name := range []string{"tala-1seed", "dagre", "elk"} {
		t.Run(name, func(t *testing.T) {
			engine, ok := EngineByName(name)
			if !ok {
				t.Fatal("unknown engine")
			}
			r := MeasureCase(t.Context(), "arch_010", source, edit, engine, time.Minute)
			if r.Err != nil || r.EditErr != nil {
				t.Fatalf("errors: %v, %v", r.Err, r.EditErr)
			}
			if r.Objects != 10 || r.Metrics.Shapes != 10 || r.Metrics.Connections != r.Edges {
				t.Fatalf("unexpected sizes: %+v", r)
			}
			if r.Layout <= 0 || r.Metrics.EdgeLength <= 0 || r.Metrics.EmptyFraction < 0 || r.Metrics.EmptyFraction >= 1 {
				t.Fatalf("implausible metrics: %+v", r)
			}
			// The two endpoints of the new edge are excluded.
			if r.Displacement == nil || r.Displacement.Compared != 8 {
				t.Fatalf("displacement: %+v", r.Displacement)
			}
		})
	}
}

// TestTALADeterministic checks that repeated layouts give identical diagrams,
// which the displacement metric relies on.
func TestTALADeterministic(t *testing.T) {
	source, _ := readCorpus(t, "arch_010")
	engine, _ := EngineByName("tala-1seed")
	var first []byte
	for i := 0; i < 2; i++ {
		run, err := Compile(context.Background(), source, engine, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(run.Diagram.Shapes)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = b
		} else if string(b) != string(first) {
			t.Fatal("TALA layout is not deterministic")
		}
	}
}

func TestCompileTimeout(t *testing.T) {
	source, _ := readCorpus(t, "arch_060")
	engine, _ := EngineByName("tala")
	_, err := Compile(context.Background(), source, engine, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), ErrTimeout.Error()) {
		t.Fatalf("got %v, want a timeout", err)
	}
}

// BenchmarkCorpus lays out corpus cases. Select cases and engines with
// prefixes, following the engine benchmark's BENCHMARK_GRAPH convention:
//
//	BENCHMARK_CORPUS=arch_060 BENCHMARK_ENGINE=tala-1seed go test ./d2layouts/d2talalayout/bench -run '^$' -bench '^BenchmarkCorpus$' -benchtime=1x
func BenchmarkCorpus(b *testing.B) {
	casePrefix := os.Getenv("BENCHMARK_CORPUS")
	engineName := os.Getenv("BENCHMARK_ENGINE")
	for _, spec := range StandardCorpus {
		if !strings.HasPrefix(spec.Name, casePrefix) {
			continue
		}
		source, _ := readCorpus(b, spec.Name)
		for _, engine := range Engines {
			if engineName != "" && engine.Name != engineName {
				continue
			}
			b.Run(spec.Name+"/"+engine.Name, func(b *testing.B) {
				var m Metrics
				for b.Loop() {
					run, err := Compile(b.Context(), source, engine, 0)
					if err != nil {
						b.Fatal(err)
					}
					m = Measure(run.Diagram)
				}
				b.ReportMetric(float64(m.Crossings), "crossings")
				b.ReportMetric(float64(m.Bends), "bends")
				b.ReportMetric(m.EdgeLength, "edge-length")
				b.ReportMetric(100*m.EmptyFraction, "empty-%")
			})
		}
	}
}
