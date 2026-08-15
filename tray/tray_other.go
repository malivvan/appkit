//go:build !darwin && !windows && !linux && !freebsd && !netbsd

package tray

func set(_ string, _ []byte, _ Config) error { return ErrUnsupported }

func remove() {}

func run(_ string, _ []byte, _ Config) error { return ErrUnsupported }

func stop() {}

func bounds() (x, y, w, h int) { return 0, 0, 0, 0 }
