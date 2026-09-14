package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestVersionOutputShape(t *testing.T) {
	version, commit, built = "1.0.0", "abc1234", "2026-09-05T00:00:00Z"
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printVersion(nil)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	got := buf.String()
	want := "tokencontrolplane 1.0.0 (commit abc1234, built 2026-09-05T00:00:00Z, schema 0)\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
