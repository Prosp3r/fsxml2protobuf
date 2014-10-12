package main

import (
	"testing"
)

func TestGetYear(t *testing.T) {
	var tests = []struct {
		in  string
		out int32
	}{
		{"", 0},
		{"1980", 1980},
		{"	1980", 1980},
		{"1980   ", 1980},
		{" 1980   ", 1980},
		{"Abt 1980", 1980},
		{"25 April 1888", 1888},
		{"3/24/2010", 2010},
		{"25Apr1888", 1888},
		{"25Apr18", 0},
		{"June", 0},
	}
	for _, test := range tests {
		actual := getYear(test.in)
		if actual != test.out {
			t.Errorf("getYear(%q) = %v; want %v", test.in, actual, test.out)
		}
	}
}
