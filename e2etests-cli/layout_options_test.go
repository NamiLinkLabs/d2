package e2etests_cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/xos"
)

func TestLayoutDataCLI(t *testing.T) {
	const body = "x -> a\nx -> b\nc: {d}\nx -> c.d\n"
	for _, test := range []struct {
		layout string
		data   string
		flag   string
		bad    string
	}{
		{layout: "elk", data: "elk-nodeNode: 80; elk-padding: 10", flag: "--elk-nodeNode=5", bad: "elk-nodeNode: lots"},
		{layout: "dagre", data: "dagre-ranksep: 40; dagre-padding: 8", flag: "--dagre-ranksep=300", bad: "dagre-padding: \"[middle=1]\""},
	} {
		t.Run(test.layout, func(t *testing.T) {
			withData := "vars: {d2-config: {data: {" + test.data + "}}}\n" + body
			plain := renderCLI(t, body, "--layout="+test.layout)
			configured := renderCLI(t, withData, "--layout="+test.layout)
			if bytes.Equal(plain, configured) {
				t.Fatal("layout data in the diagram did not change the output")
			}
			// The diagram's values take precedence over the flag.
			if got := renderCLI(t, withData, "--layout="+test.layout, test.flag); !bytes.Equal(got, configured) {
				t.Fatalf("%s overrode the diagram's layout data", test.flag)
			}

			directory := t.TempDir()
			writeFile(t, directory, "index.d2", "vars: {d2-config: {data: {"+test.bad+"}}}\n"+body)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			state := testMain(directory, xos.NewEnv(nil), "--layout="+test.layout, "index.d2", "output.svg")
			var stderr bytes.Buffer
			state.Stderr = &stderr
			state.Start(t, ctx)
			defer state.Cleanup(t)
			err := state.Wait(ctx)
			if err == nil {
				t.Fatal("invalid layout data compiled")
			}
			key, _, _ := strings.Cut(test.bad, ":")
			if msg := err.Error() + stderr.String(); !strings.Contains(msg, `index.d2:1:27: `) || !strings.Contains(msg, `"`+key+`"`) {
				t.Fatalf("error does not point at %s:\n%s", key, msg)
			}
		})
	}
}

func renderCLI(t *testing.T, source string, args ...string) []byte {
	t.Helper()
	directory := t.TempDir()
	writeFile(t, directory, "index.d2", source)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	state := testMain(directory, xos.NewEnv(nil), append(append([]string{"--omit-version"}, args...), "index.d2", "output.svg")...)
	var stderr bytes.Buffer
	state.Stderr = &stderr
	state.Start(t, ctx)
	defer state.Cleanup(t)
	if err := state.Wait(ctx); err != nil {
		t.Fatalf("CLI compile failed: %v\nstderr:\n%s", err, stderr.String())
	}
	return readFile(t, directory, "output.svg")
}
