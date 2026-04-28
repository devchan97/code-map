package pipeline

import (
	"fmt"
	"io"
	"sync/atomic"

	"github.com/devchan97/code-map/internal/platform"
)

// Progress is a minimal progress reporter that writes status to an io.Writer
// when that writer is a TTY. All counter methods are safe for concurrent use.
type Progress struct {
	out    io.Writer
	isTerm bool
	total  int

	parsed  atomic.Int64
	skipped atomic.Int64
}

// NewProgress returns a Progress reporter that prints status to w when w is a
// terminal (detected via platform.IsTerminal). Pass total = 0 if the total
// number of files is unknown.
func NewProgress(w io.Writer, total int) *Progress {
	p := &Progress{
		out:   w,
		total: total,
	}

	// Detect whether w is a TTY using its file descriptor, if available.
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		p.isTerm = platform.IsTerminal(f.Fd())
	}

	return p
}

// IncParsed records a successfully parsed file and may print a status line.
func (p *Progress) IncParsed() {
	n := p.parsed.Add(1)
	p.print(n)
}

// IncSkipped records a skipped file and may print a status line.
func (p *Progress) IncSkipped() {
	p.skipped.Add(1)
	p.print(p.parsed.Load())
}

// Finish prints a final newline to terminate the in-place status line, if any
// output was previously written.
func (p *Progress) Finish() {
	if !p.isTerm {
		return
	}
	total := p.parsed.Load() + p.skipped.Load()
	if total == 0 {
		return
	}
	fmt.Fprintln(p.out)
}

// print writes an in-place status line when the writer is a TTY.
func (p *Progress) print(parsed int64) {
	if !p.isTerm {
		return
	}
	skipped := p.skipped.Load()
	if p.total > 0 {
		fmt.Fprintf(p.out, "\rparsed %d / %d  skipped %d", parsed, p.total, skipped)
	} else {
		fmt.Fprintf(p.out, "\rparsed %d  skipped %d", parsed, skipped)
	}
}
