package ufdr

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestFixture(t *testing.T) {
	b, e := os.ReadFile("../../docs/research/fixtures/ufdr/messages-minimal.xml")
	if e != nil {
		t.Fatal(e)
	}
	r, e := Normalize(b)
	if e != nil {
		t.Fatal(e)
	}
	want, e := os.ReadFile("../../docs/research/fixtures/ufdr/messages-minimal.expected.json")
	if e != nil {
		t.Fatal(e)
	}
	got, _ := json.Marshal(r)
	var a, c any
	json.Unmarshal(want, &a)
	json.Unmarshal(got, &c)
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("mismatch: %s", got)
	}
	r, e = Normalize([]byte(strings.ReplaceAll(string(b), "Meet at the library at noon.", "Changed body")))
	if e != nil || *r.Messages[0].Body != "Changed body" {
		t.Fatal("body was not parsed")
	}
}
func TestInvalid(t *testing.T) {
	for _, s := range []string{"<", "<project/>"} {
		if _, e := Normalize([]byte(s)); e == nil {
			t.Fatal("accepted invalid input")
		}
	}
}

func TestReportSuffix(t *testing.T) {
	input, err := os.ReadFile("../../docs/research/fixtures/ufdr/messages-minimal.xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		suffix string
		valid  bool
	}{
		{" \n\t<!-- end --><?review done?>\r\n", true},
		{"<broken", false},
		{"<second/>", false},
		{"text", false},
		{"<!-- unfinished", false},
		{"<!DOCTYPE project>", false},
		{`<?xml version="1.0"?>`, false},
		{`<?XML version="1.0"?>`, false},
	} {
		t.Run(tc.suffix, func(t *testing.T) {
			result, err := Normalize([]byte(string(input) + tc.suffix))
			if tc.valid {
				if err != nil || len(result.Messages) != 1 {
					t.Fatalf("valid suffix changed the report: %v, %v", result, err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid report suffix")
			}
		})
	}
}
