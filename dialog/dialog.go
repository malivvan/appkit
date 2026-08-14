// Package dialog shows native file and folder pickers (GTK, Win32, or AppKit)
// behind one Options type. A dialog that cannot run on the current platform
// returns an empty result rather than an error.
package dialog

import (
	"fmt"
	"strings"
)

// Type selects which kind of picker Open shows.
type Type string

const (
	// TypeOpen picks a single existing file (the default when Type is empty).
	TypeOpen Type = "open"

	// TypeOpenMultiple picks one or more existing files.
	TypeOpenMultiple Type = "open-multiple"

	// TypeSave picks a (possibly new) file to write.
	TypeSave Type = "save"

	// TypeDirectory picks a folder.
	TypeDirectory Type = "directory"
)

// FileFilter is a named group of extensions ("Images", "png", "jpg").
type FileFilter struct {
	Name string

	Extensions []string
}

// Options configures a dialog. Extensions and Filters are matched case-
// insensitively; a leading dot and the "*" wildcard are accepted and ignored.
type Options struct {
	Type Type

	Title string

	Directory string

	Filename string

	Extensions []string

	Filters []FileFilter
}

// Open shows the picker described by opts and returns the chosen paths, or nil
// if the user cancelled.
func Open(opts Options) ([]string, error) {
	switch opts.Type {
	case "", TypeOpen:
		return onePath(open(opts)), nil
	case TypeOpenMultiple:
		return openMultiple(opts), nil
	case TypeSave:
		return onePath(save(opts)), nil
	case TypeDirectory:
		return onePath(pickDirectory(opts)), nil
	default:
		return nil, fmt.Errorf("dialog: unsupported dialog type %q", opts.Type)
	}
}

func onePath(path string) []string {
	if path == "" {
		return nil
	}
	return []string{path}
}

// normalizeExtensions trims leading dots and rejects wildcard patterns, so the
// platform backends always receive plain extension names.
func normalizeExtensions(exts []string) []string {
	clean := make([]string, 0, len(exts))
	for _, e := range exts {
		e = strings.TrimPrefix(e, ".")
		if e == "" || e == "*" {
			return nil
		}
		clean = append(clean, e)
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// extensionList flattens Filters (preferred) or falls back to Extensions.
func extensionList(opts Options) []string {
	var all []string
	for _, f := range opts.Filters {
		all = append(all, f.Extensions...)
	}
	if len(all) > 0 {
		return normalizeExtensions(all)
	}
	return normalizeExtensions(opts.Extensions)
}

func firstOrEmpty(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
