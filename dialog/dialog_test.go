package dialog

import (
	"slices"
	"testing"
)

func TestCleanExtensions(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{"plain", []string{"png", "jpg"}, []string{"png", "jpg"}},
		{"leading dots stripped", []string{".afoil", ".dat"}, []string{"afoil", "dat"}},
		{"wildcard star disables", []string{"png", "*"}, nil},
		{"wildcard empty disables", []string{"png", ""}, nil},
		{"lone dot disables", []string{"."}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeExtensions(c.in)
			if !slices.Equal(got, c.want) {
				t.Fatalf("cleanExtensions(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestFilterExtensions(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want []string
	}{
		{"zero options", Options{}, nil},
		{"extensions only", Options{Extensions: []string{"png", "jpg"}}, []string{"png", "jpg"}},
		{"filters win over extensions", Options{
			Extensions: []string{"png"},
			Filters:    []FileFilter{{Name: "Docs", Extensions: []string{"md", ".txt"}}},
		}, []string{"md", "txt"}},
		{"filters merged across entries", Options{
			Filters: []FileFilter{{Extensions: []string{"a"}}, {Extensions: []string{"b"}}},
		}, []string{"a", "b"}},
		{"filter wildcard disables", Options{
			Filters: []FileFilter{{Extensions: []string{"a"}}, {Extensions: []string{"*"}}},
		}, nil},
		{"extensions wildcard disables with filters absent", Options{Extensions: []string{"a", ""}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extensionList(c.opts)
			if !slices.Equal(got, c.want) {
				t.Fatalf("filterExtensions(%+v) = %q, want %q", c.opts, got, c.want)
			}
		})
	}
}
