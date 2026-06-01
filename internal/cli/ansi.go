package cli

import (
	"io"
	"os"
)

// ANSI terminal escape sequences for formatting chat output.
// See https://en.wikipedia.org/wiki/ANSI_escape_code.
const (
	ansiReset     = "\033[0m"
	ansiBold      = "\033[1m"
	ansiDim       = "\033[2m"
	ansiGreen     = "\033[32m"
	ansiCyan      = "\033[36m"
	ansiYellow    = "\033[33m"
	ansiRed       = "\033[31m"
	ansiBoldCyan  = "\033[1;36m"
	ansiBoldGreen = "\033[1;32m"
	ansiBoldRed   = "\033[1;31m"
)

// colorEnabled reports whether the terminal likely supports ANSI escape codes.
// Respects the NO_COLOR convention (https://no-color.org/) and TERM=dumb.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	t := os.Getenv("TERM")
	return t != "" && t != "dumb"
}

// isTTY reports whether w is an interactive terminal (character device).
// Returns false for files, pipes, and non-*os.File writers (e.g. test buffers).
// Used to auto-disable pretty/ANSI output when stdout is redirected to a file.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// ansiWrap surrounds s with an ANSI start code and reset when colors are enabled.
func ansiWrap(code, s string) string {
	if !colorEnabled() {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string      { return ansiWrap(ansiBold, s) }
func dim(s string) string       { return ansiWrap(ansiDim, s) }
func green(s string) string     { return ansiWrap(ansiGreen, s) }
func cyan(s string) string      { return ansiWrap(ansiCyan, s) }
func yellow(s string) string    { return ansiWrap(ansiYellow, s) }
func red(s string) string       { return ansiWrap(ansiRed, s) }
func boldCyan(s string) string  { return ansiWrap(ansiBoldCyan, s) }
func boldGreen(s string) string { return ansiWrap(ansiBoldGreen, s) }
func boldRed(s string) string   { return ansiWrap(ansiBoldRed, s) }
