package main

import (
	"encoding/xml"
	"github.com/DallanQ/fsxml2protobuf/fs_data"
	"strings"
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

func TestGetGender(t *testing.T) {
	var tests = []struct {
		in  string
		out fs_data.FSGender
	}{
		{`<person><gender type="http://gedcomx.org/Male"/></person>`, fs_data.FSGender_MALE},
		{`<person><gender type="http://gedcomx.org/Female"/></person>`, fs_data.FSGender_FEMALE},
		{`<person><gender/></person>`, fs_data.FSGender_UNKNOWN},
		{`<person></person>`, fs_data.FSGender_UNKNOWN},
	}
	for _, test := range tests {
		var person Person
		err := xml.NewDecoder(strings.NewReader(test.in)).Decode(&person)
		if err != nil {
			t.Errorf("Error decoding %s %v", test.in, err)
		} else {
			actual := getGender(&person)
			if actual != test.out {
				t.Errorf("getGender(%q) = %v; want %v", test.in, actual, test.out)
			}
		}
	}
}
