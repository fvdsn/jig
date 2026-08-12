package jig

import (
	"bytes"
	"strings"
	"testing"
)

func TestProgressRendersTransientStatusLine(t *testing.T) {
	var status bytes.Buffer
	p := &progress{out: &status, width: 40, total: 2, active: map[string]bool{}}

	p.start("a/one")
	p.start("b/two")
	if got := status.String(); !strings.Contains(got, "[0/2] a/one b/two") {
		t.Fatalf("status = %q, want both active entries", got)
	}

	var out bytes.Buffer
	p.println(&out, "lint: a/one")
	if out.String() != "lint: a/one\n" {
		t.Fatalf("out = %q, want the plain completed line", out.String())
	}
	p.finish("a/one")
	if got := status.String(); !strings.Contains(got, "[1/2] b/two") {
		t.Fatalf("status = %q, want done counter and remaining entry", got)
	}

	p.close()
	if !strings.HasSuffix(status.String(), "\r\033[K") {
		t.Fatalf("status = %q, want trailing erase", status.String())
	}

	// Long lines are clipped to the terminal width.
	status.Reset()
	q := &progress{out: &status, width: 12, total: 1, active: map[string]bool{}}
	q.start("a/very/long/path/name")
	lines := strings.Split(status.String(), "\r\033[K")
	if last := lines[len(lines)-1]; len(last) >= 12 {
		t.Fatalf("rendered %q, want clipped below width", last)
	}
}

func TestProgressIsSilentWithoutTerminal(t *testing.T) {
	var status bytes.Buffer
	p := &progress{out: &status, width: 0, total: 1, active: map[string]bool{}}
	p.start("a")
	var out bytes.Buffer
	p.println(&out, "lint: a")
	p.finish("a")
	p.close()
	if status.Len() != 0 {
		t.Fatalf("status = %q, want nothing without a terminal", status.String())
	}
	if out.String() != "lint: a\n" {
		t.Fatalf("out = %q", out.String())
	}
}
