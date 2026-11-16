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
