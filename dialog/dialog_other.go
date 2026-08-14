//go:build !darwin && !windows && !linux && !freebsd && !netbsd

package dialog

func open(_ Options) string { return "" }

func openMultiple(_ Options) []string { return nil }

func save(_ Options) string { return "" }

func pickDirectory(_ Options) string { return "" }
