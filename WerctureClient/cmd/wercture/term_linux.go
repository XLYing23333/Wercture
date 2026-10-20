//go:build linux

package main

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type terminal interface {
	Key() (string, error)
	Size() (int, int)
	Write(string)
	Close() error
}

type linuxTerm struct {
	fd       int
	original *unix.Termios
	buffer   []byte
}

func openTerminal() (terminal, error) {
	if _, err := unix.IoctlGetTermios(int(os.Stdout.Fd()), unix.TCGETS); err != nil {
		return nil, err
	}
	original, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	if err != nil {
		return nil, err
	}
	term := &linuxTerm{fd: int(os.Stdin.Fd()), original: original}
	raw := *original
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(term.fd, unix.TCSETS, &raw); err != nil {
		return nil, err
	}
	return term, nil
}

func (t *linuxTerm) Close() error {
	t.Write("\x1b[?25h")
	return unix.IoctlSetTermios(t.fd, unix.TCSETS, t.original)
}

func (t *linuxTerm) Size() (int, int) {
	s, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || s.Row == 0 || s.Col == 0 {
		return 24, 80
	}
	return int(s.Row), int(s.Col)
}

func (t *linuxTerm) Write(text string) {
	_, _ = os.Stdout.WriteString(text)
}

func (t *linuxTerm) Key() (string, error) {
	kind, r, err := t.next()
	if err != nil || kind != "char" {
		return kind, err
	}
	return string(r), nil
}

// next reads one key. A lone Esc is returned after a short wait, so it is not held until the next key arrives.
func (t *linuxTerm) next() (string, rune, error) {
	for {
		if len(t.buffer) == 0 {
			if err := t.fill(-1); err != nil {
				return "", 0, err
			}
		}
		b := t.buffer[0]
		if b == 27 {
			if len(t.buffer) == 1 {
				if err := t.fill(50); err != nil {
					return "", 0, err
				}
			}
			if len(t.buffer) > 1 && len(t.buffer) < 3 && (t.buffer[1] == '[' || t.buffer[1] == 'O') {
				if err := t.fill(50); err != nil {
					return "", 0, err
				}
			}
			for _, seq := range []string{"\x1b[A", "\x1bOA", "\x1b[B", "\x1bOB"} {
				if strings.HasPrefix(string(t.buffer), seq) {
					t.buffer = t.buffer[len(seq):]
					if seq[len(seq)-1] == 'A' {
						return "up", 0, nil
					}
					return "down", 0, nil
				}
			}
			t.buffer = t.buffer[1:]
			return "esc", 0, nil
		}
		t.buffer = t.buffer[1:]
		switch b {
		case '\n', '\r':
			return "enter", 0, nil
		case 127, 8:
			return "backspace", 0, nil
		}
		if b < 32 {
			continue
		}
		t.buffer = append([]byte{b}, t.buffer...)
		if !utf8.FullRune(t.buffer) {
			if err := t.fill(50); err != nil {
				return "", 0, err
			}
			if !utf8.FullRune(t.buffer) {
				t.buffer = t.buffer[1:]
				continue
			}
		}
		r, n := utf8.DecodeRune(t.buffer)
		t.buffer = t.buffer[n:]
		if r == utf8.RuneError {
			continue
		}
		return "char", r, nil
	}
}

func (t *linuxTerm) fill(timeout int) error {
	fds := []unix.PollFd{{Fd: int32(t.fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, timeout)
	if err == unix.EINTR || n == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if fds[0].Revents&(unix.POLLHUP|unix.POLLERR) != 0 {
		return io.EOF
	}
	chunk := make([]byte, 64)
	n, err = unix.Read(t.fd, chunk)
	if err != nil {
		return err
	}
	if n == 0 {
		return io.EOF
	}
	t.buffer = append(t.buffer, chunk[:n]...)
	return nil
}
