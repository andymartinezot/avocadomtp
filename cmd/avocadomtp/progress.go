package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

var stderrIsTTY = func() bool {
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

// progress prints a single updating line for one transfer.
type progress struct {
	name  string
	total int64
	done  int64
	start time.Time
	last  time.Time
}

func newProgress(name string, total int64) *progress {
	return &progress{name: name, total: total, start: time.Now()}
}

func (p *progress) add(n int) {
	p.done += int64(n)
	if now := time.Now(); stderrIsTTY && now.Sub(p.last) > 150*time.Millisecond {
		p.last = now
		p.print()
	}
}

func (p *progress) print() {
	pct := 100.0
	if p.total > 0 {
		pct = float64(p.done) * 100 / float64(p.total)
	}
	fmt.Fprintf(os.Stderr, "\r\033[K%-40s %5.1f%%  %9s / %-9s  %s/s",
		truncate(p.name, 40), pct, human(p.done), human(p.total), human(int64(p.rate())))
}

func (p *progress) rate() float64 {
	el := time.Since(p.start).Seconds()
	if el <= 0 {
		return 0
	}
	return float64(p.done) / el
}

// finish prints the final line.
func (p *progress) finish(verb string) {
	if stderrIsTTY {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
	fmt.Fprintf(os.Stderr, "%s %s (%s, %s/s)\n", verb, p.name, human(p.done), human(int64(p.rate())))
}

func (p *progress) writer(w io.Writer) io.Writer { return &progWriter{w, p} }
func (p *progress) reader(r io.Reader) io.Reader { return &progReader{r, p} }

type progWriter struct {
	w io.Writer
	p *progress
}

func (pw *progWriter) Write(b []byte) (int, error) {
	n, err := pw.w.Write(b)
	pw.p.add(n)
	return n, err
}

type progReader struct {
	r io.Reader
	p *progress
}

func (pr *progReader) Read(b []byte) (int, error) {
	n, err := pr.r.Read(b)
	pr.p.add(n)
	return n, err
}

func human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}
