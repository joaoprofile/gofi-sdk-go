package connection

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var queryPrint atomic.Pointer[queryPrinter]

// PrintQueries writes, to w, every statement sqln runs (see LogQueries) laid
// out for reading — FormatSQL, then one argument per line — framed so each
// stands apart in the console; nil stops it. Keywords, parameters and
// literals are colored when w is a terminal and NO_COLOR is unset. It is
// independent of LogQueries, and as with it the arguments may hold personal
// data: development only (gofi refuses it in stage and prod).
func PrintQueries(w io.Writer) {
	if w == nil {
		queryPrint.Store(nil)
		return
	}
	queryPrint.Store(&queryPrinter{w: w, color: isTerminal(w) && os.Getenv("NO_COLOR") == ""})
}

type queryPrinter struct {
	mu    sync.Mutex // one block at a time: concurrent queries do not interleave
	w     io.Writer
	color bool
}

// ANSI styles.
const (
	ansiReset   = "\033[0m"
	ansiDim     = "\033[2m"
	ansiBold    = "\033[1m"
	ansiKeyword = "\033[1;36m" // bold cyan
	ansiParam   = "\033[33m"   // yellow
	ansiString  = "\033[32m"   // green
	ansiError   = "\033[1;31m" // bold red
)

func (p *queryPrinter) style(code, s string) string {
	if !p.color {
		return s
	}
	return code + s + ansiReset
}

func (p *queryPrinter) print(op, query string, args []any, elapsed time.Duration, err error) {
	f := &sqlFormatter{toks: tokenizeSQL(query)}
	if p.color {
		f.paint = func(t sqlTok, keyword bool) string {
			switch {
			case keyword:
				return ansiKeyword + t.s + ansiReset
			case t.kind == tokString:
				return ansiString + t.s + ansiReset
			case t.kind == tokWord && (strings.HasPrefix(t.s, "$") || t.s == "?"),
				t.kind == tokOp && t.s == "?":
				return ansiParam + t.s + ansiReset
			}
			return t.s
		}
	}
	body := query
	if utf8.ValidString(query) {
		f.all()
		body = f.b.String()
	}

	bar := p.style(ansiDim, "│ ")
	var b strings.Builder
	header := fmt.Sprintf("sql · %s · %s", op, elapsed.Round(time.Microsecond))
	b.WriteString(p.style(ansiDim, "┌─ ") + p.style(ansiBold, header) + "\n")
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		b.WriteString(bar + line + "\n")
	}
	if len(args) > 0 {
		b.WriteString(p.style(ansiDim, "├─ args") + "\n")
		for i, a := range args {
			name := fmt.Sprintf("$%d", i+1)
			b.WriteString(bar + "  " + p.style(ansiParam, name) + " = " + formatArg(a) + "\n")
		}
	}
	if err != nil {
		b.WriteString(p.style(ansiDim, "├─ ") + p.style(ansiError, "error: "+err.Error()) + "\n")
	}
	b.WriteString(p.style(ansiDim, "└─") + "\n")

	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = io.WriteString(p.w, b.String())
}

// isTerminal reports whether w is a character device, such as a console.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
