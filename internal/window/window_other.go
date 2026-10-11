//go:build !windows

// Package window resizes the terminal window that hosts this process.
package window

import "errors"

// Fit is only implemented on Windows.
func Fit(aw, ah int, dock string) (restore func(), err error) {
	return nil, errors.New("window resizing is only supported on Windows")
}
