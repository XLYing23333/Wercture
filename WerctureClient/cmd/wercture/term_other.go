//go:build !linux

package main

import "errors"

type terminal interface {
	Key() (string, error)
	Size() (int, int)
	Write(string)
	Close() error
}

func openTerminal() (terminal, error) {
	return nil, errors.New("interactive terminal required")
}
