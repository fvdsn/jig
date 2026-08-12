package jig

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
)

// progress renders a transient status line while parallel work runs, so
// long commands do not look stuck: a done counter plus the entries
// currently in flight. It writes to stderr and stays silent when stderr is
// not a terminal, keeping piped output clean.
type progress struct {
	mu     sync.Mutex
	out    io.Writer
	width  int // 0 disables rendering
	total  int
	done   int
	active map[string]bool
}

func newProgress(total int) *progress {
	return &progress{out: os.Stderr, width: termWidth(os.Stderr), total: total, active: map[string]bool{}}
}

func (p *progress) start(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active[name] = true
	p.render()
}

func (p *progress) finish(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, name)
	p.done++
	p.render()
}

// println clears the status line, prints a completed line to w, and redraws
// the status below it, so persistent output and the transient line share a
// terminal without interleaving.
func (p *progress) println(w io.Writer, line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.erase()
	fmt.Fprintln(w, line)
	p.render()
}

// close erases the status line for good; call before printing summaries.
func (p *progress) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.erase()
	p.width = 0
}

func (p *progress) erase() {
	if p.width > 0 {
		fmt.Fprint(p.out, "\r\033[K")
	}
}

func (p *progress) render() {
	if p.width == 0 {
		return
	}
	names := make([]string, 0, len(p.active))
	for name := range p.active {
		names = append(names, name)
	}
	sort.Strings(names)
	line := fmt.Sprintf("[%d/%d] %s", p.done, p.total, strings.Join(names, " "))
	if len(line) >= p.width {
		line = line[:p.width-1]
	}
	fmt.Fprint(p.out, "\r\033[K"+line)
}
