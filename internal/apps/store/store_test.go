package store

import "testing"

func TestRoundTrip(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var v struct{ A []string }
	if err := Load("x", &v); err != nil || v.A != nil {
		t.Fatalf("missing file: %v %v", v, err)
	}
	v.A = []string{"a", "b"}
	if err := Save("x", v); err != nil {
		t.Fatal(err)
	}
	var w struct{ A []string }
	if err := Load("x", &w); err != nil || len(w.A) != 2 || w.A[1] != "b" {
		t.Fatalf("read back %v %v", w, err)
	}
}
