//go:build !linux

package main

import "errors"

type termios struct{ Lflag uint32 }

func termiosGet(int) (termios, error) { return termios{}, errors.New("terminal unsupported") }

func termiosSet(int, termios) error { return errors.New("terminal unsupported") }
