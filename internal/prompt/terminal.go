// Package prompt adds small terminal selectors with a plain numbered fallback.
package prompt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

type Terminal struct {
	ctx      context.Context
	in, out  *os.File
	reader   *bufio.Reader
	rich     bool
	original *term.State
}

func New(ctx context.Context, in, out *os.File, plain bool) *Terminal {
	tty := IsTerminal(in)
	t := &Terminal{ctx: ctx, in: in, out: out, reader: bufio.NewReader(singleByteReader{in})}
	t.rich = tty && IsTerminal(out) && !plain && os.Getenv("TERM") != "dumb" && os.Getenv("TERM") != ""
	if tty {
		t.original, _ = term.GetState(int(in.Fd()))
	}
	return t
}
func IsTerminal(file *os.File) bool { return term.IsTerminal(int(file.Fd())) }
func (t *Terminal) Close() {
	if t.original != nil {
		_ = term.Restore(int(t.in.Fd()), t.original)
	}
	if t.rich {
		fmt.Fprint(t.out, "\x1b[?25h")
	}
}

type answer[T any] struct {
	value T
	err   error
}

func await[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	done := make(chan answer[T], 1)
	go func() { value, err := fn(); done <- answer[T]{value, err} }()
	select {
	case result := <-done:
		return result.value, result.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

func (t *Terminal) Choose(label string, items []string, initial int) (int, error) {
	if t.rich {
		return t.chooseArrows(label, items, initial)
	}
	fmt.Fprintln(t.out, label)
	for i, item := range items {
		fmt.Fprintf(t.out, "  %d) %s\n", i+1, item)
	}
	for {
		fmt.Fprintf(t.out, "Choice [%d]: ", initial+1)
		value, err := t.line()
		if err != nil {
			return 0, err
		}
		if value == "" {
			return initial, nil
		}
		index, err := strconv.Atoi(value)
		if err == nil && index >= 1 && index <= len(items) {
			return index - 1, nil
		}
		fmt.Fprintln(t.out, "Select a listed number.")
	}
}

func (t *Terminal) chooseArrows(label string, items []string, selected int) (int, error) {
	state, err := term.MakeRaw(int(t.in.Fd()))
	if err != nil {
		return 0, err
	}
	defer term.Restore(int(t.in.Fd()), state)
	fmt.Fprintf(t.out, "%s (↑/↓, Enter; Esc cancels)\r\n\x1b[?25l", label)
	defer fmt.Fprint(t.out, "\x1b[?25h")
	draw := func() {
		for i, item := range items {
			marker := " "
			if i == selected {
				marker = ">"
			}
			fmt.Fprintf(t.out, "\r %s %s\x1b[K\r\n", marker, item)
		}
	}
	draw()
	for {
		key, err := await(t.ctx, t.reader.ReadByte)
		if err != nil {
			return 0, err
		}
		switch key {
		case '\r', '\n':
			return selected, nil
		case 3, 4, 'q':
			return 0, context.Canceled
		case 27:
			ctx, cancel := context.WithTimeout(t.ctx, 100*time.Millisecond)
			next, err := await(ctx, t.reader.ReadByte)
			if err != nil || next != '[' {
				cancel()
				return 0, context.Canceled
			}
			direction, err := await(ctx, t.reader.ReadByte)
			cancel()
			if err != nil {
				return 0, context.Canceled
			}
			switch direction {
			case 'A':
				selected = (selected + len(items) - 1) % len(items)
			case 'B':
				selected = (selected + 1) % len(items)
			default:
				continue
			}
			fmt.Fprintf(t.out, "\x1b[%dA", len(items))
			draw()
		}
	}
}

func (t *Terminal) Input(label string, hidden bool) (string, error) {
	if t.rich {
		state, err := term.MakeRaw(int(t.in.Fd()))
		if err != nil {
			return "", err
		}
		defer term.Restore(int(t.in.Fd()), state)
		terminal := term.NewTerminal(terminalIO{interruptReader{t.in}, t.out}, label+": ")
		if hidden {
			return await(t.ctx, func() (string, error) { return terminal.ReadPassword(label + ": ") })
		}
		return await(t.ctx, terminal.ReadLine)
	}
	fmt.Fprintf(t.out, "%s: ", label)
	if hidden && IsTerminal(t.in) {
		data, err := await(t.ctx, func() ([]byte, error) { return term.ReadPassword(int(t.in.Fd())) })
		fmt.Fprintln(t.out)
		return string(data), err
	}
	return t.line()
}
func (t *Terminal) Summary(lines []string) {
	for _, line := range lines {
		fmt.Fprintln(t.out, line)
	}
}
func (t *Terminal) line() (string, error) {
	return await(t.ctx, func() (string, error) {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
	})
}
func IsCanceled(err error) bool { return errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) }

type singleByteReader struct{ io.Reader }

func (r singleByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

type terminalIO struct {
	io.Reader
	io.Writer
}
type interruptReader struct{ io.Reader }

func (r interruptReader) Read(p []byte) (int, error) {
	n, err := (singleByteReader{r.Reader}).Read(p)
	if n > 0 && p[0] == 3 {
		return 0, context.Canceled
	}
	return n, err
}
