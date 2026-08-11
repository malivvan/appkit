//go:build !darwin && !windows && !linux && !freebsd && !netbsd

package notify

func show(_, _, _ string, _ Options) error { return ErrUnsupported }

func beep(_ float64, _ int) error { return ErrUnsupported }

func alertSound() error { return nil }
