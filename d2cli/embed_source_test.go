package d2cli

import (
	"context"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/lib/sourcemeta"
)

func TestFormatBoardTargetRoundTrips(t *testing.T) {
	for _, boardPath := range [][]string{
		{"layers", "x"},
		{"layers", "x", "scenarios", "y z"},
		{"steps", "a.b"},
		{"layers", "label"},
		{"layers", `quo"te`},
	} {
		target := formatBoardTarget(boardPath)
		key, err := d2parser.ParseKey(target)
		if err != nil {
			t.Fatalf("%v: %q does not parse: %v", boardPath, target, err)
		}
		if got := key.StringIDA(); !reflect.DeepEqual(got, boardPath) {
			t.Fatalf("%v: %q parses to %v", boardPath, target, got)
		}
	}
	if got := formatBoardTarget(nil); got != "" {
		t.Fatalf("root target = %q", got)
	}
}

func TestOutputBundleTargets(t *testing.T) {
	ctx := context.Background()
	if outputBundle(ctx) != nil {
		t.Fatal("bundle without --embed-source")
	}
	base := &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "a.d2", Content: "a"}}
	target := func(o *embeddedOutput, boardPath ...string) *string {
		ctx := withEmbeddedBoard(withEmbeddedOutput(ctx, o), nil)
		for i := 0; i+1 < len(boardPath); i += 2 {
			ctx = withEmbeddedChildBoard(ctx, boardPath[i], boardPath[i+1])
		}
		return outputBundle(ctx).Target
	}
	if got := target(&embeddedOutput{bundle: base, target: "*"}); got != nil {
		t.Fatalf("single board target = %q", *got)
	}
	if got := target(&embeddedOutput{bundle: base, multiBoard: true, target: "*"}); got == nil || *got != "" {
		t.Fatalf("root of a multi-board export = %v", got)
	}
	if got := target(&embeddedOutput{bundle: base, multiBoard: true, target: "*"}, "layers", "x", "steps", "1"); got == nil || *got != "layers.x.steps.1" {
		t.Fatalf("child board = %v", got)
	}
	if got := target(&embeddedOutput{bundle: base, multiBoard: true, animated: true, target: "*"}); got != nil {
		t.Fatalf("animated whole diagram = %q", *got)
	}
	if got := target(&embeddedOutput{bundle: base, multiBoard: true, animated: true, target: "layers.x.*"}); got == nil || *got != "layers.x.*" {
		t.Fatalf("animated subtree = %v", got)
	}
	if base.Target != nil {
		t.Fatal("outputBundle changed the shared bundle")
	}
}
