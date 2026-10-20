package main

import "golang.org/x/sys/unix"

func termiosGet(fd int) (unix.Termios, error) {
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return unix.Termios{}, err
	}
	return *state, nil
}

func termiosSet(fd int, state unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETS, &state)
}
