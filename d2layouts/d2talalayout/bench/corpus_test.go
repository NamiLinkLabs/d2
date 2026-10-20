package bench

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

var updateCorpus = flag.Bool("update", false, "rewrite the generated corpus under testdata/corpus")

const corpusDir = "testdata/corpus"

func corpusPath(name string) string {
	return filepath.Join(corpusDir, name+".d2")
}

// TestCorpusFiles keeps the committed corpus identical to the generator.
func TestCorpusFiles(t *testing.T) {
	for _, spec := range StandardCorpus {
		source, edit, err := Generate(spec)
		if err != nil {
			t.Fatal(err)
		}
		again, againEdit, err := Generate(spec)
		if err != nil {
			t.Fatal(err)
		}
		if again != source || againEdit != edit {
			t.Fatalf("%s: generator is not deterministic", spec.Name)
		}
		path := corpusPath(spec.Name)
		if *updateCorpus {
			if err := os.MkdirAll(corpusDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		committed, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (regenerate with -update)", spec.Name, err)
		}
		if string(committed) != source {
			t.Fatalf("%s is stale; regenerate with: go test ./d2layouts/d2talalayout/bench -run TestCorpusFiles -update", path)
		}
		if got, ok := EditFromSource(string(committed)); !ok || got != edit {
			t.Fatalf("%s: recorded edit %q, want %q", spec.Name, got, edit)
		}
	}
}

// TestCorpusShape checks that every corpus diagram compiles to the intended
// size and nesting, and that its edit adds exactly one new edge between
// existing objects.
func TestCorpusShape(t *testing.T) {
	for _, spec := range StandardCorpus {
		t.Run(spec.Name, func(t *testing.T) {
			source, edit, err := Generate(spec)
			if err != nil {
				t.Fatal(err)
			}
			g, _, err := d2compiler.Compile("", strings.NewReader(source), &d2compiler.CompileOptions{Context: context.Background()})
			if err != nil {
				t.Fatal(err)
			}
			if len(g.Objects) != spec.Objects {
				t.Fatalf("got %d objects, want %d", len(g.Objects), spec.Objects)
			}
			containers, maxLevel := 0, 0
			for _, obj := range g.Objects {
				if len(obj.ChildrenArray) > 0 {
					containers++
					maxLevel = max(maxLevel, int(obj.Level()))
				}
			}
			if containers == 0 {
				t.Fatal("corpus diagram has no containers")
			}
			// Containers are nested at most two levels deep.
			if maxLevel > 2 {
				t.Fatalf("container nesting level %d exceeds 2", maxLevel)
			}
			if len(g.Edges) < spec.Objects*3/4 {
				t.Fatalf("only %d edges for %d objects", len(g.Edges), spec.Objects)
			}
			edited, _, err := d2compiler.Compile("", strings.NewReader(ApplyEdit(source, edit)), &d2compiler.CompileOptions{Context: context.Background()})
			if err != nil {
				t.Fatal(err)
			}
			if len(edited.Objects) != len(g.Objects) || len(edited.Edges) != len(g.Edges)+1 {
				t.Fatalf("edit %q changed objects %d->%d, edges %d->%d", edit, len(g.Objects), len(edited.Objects), len(g.Edges), len(edited.Edges))
			}
		})
	}
}
