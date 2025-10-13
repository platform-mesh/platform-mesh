package subroutines

import (
	"testing"
)

func TestValidateCreator(t *testing.T) {
	cases := []struct {
		in    string
		valid bool
	}{
		{"alice", true},
		{"system:serviceaccount:ns:sa", false},
		{"system:serviceaccount:", false},
	}
	for _, c := range cases {
		if got := validateCreator(c.in); got != c.valid {
			t.Fatalf("validateCreator(%s)=%v want %v", c.in, got, c.valid)
		}
	}
}

func TestFormatUser(t *testing.T) {
	in := "system:serviceaccount:ns:name"
	want := "system.serviceaccount.ns.name"
	if got := formatUser(in); got != want {
		t.Fatalf("formatUser mismatch got %s want %s", got, want)
	}
	// non-service account untouched
	plain := "bob"
	if formatUser(plain) != plain {
		t.Fatalf("formatUser should not modify %s", plain)
	}
}
