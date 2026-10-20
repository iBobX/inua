package hypr

import "testing"

func TestFloatingOf(t *testing.T) {
	clients := []byte(`[
		{"address":"0xa","pid":7,"mapped":true,"floating":false},
		{"address":"0xb","pid":9,"mapped":true,"floating":true},
		{"address":"0xc","pid":11,"mapped":true,"floating":false}]`)
	if a, err := floatingOf(clients, 9); err != nil || a != NormalizeAddress("0xb") {
		t.Fatalf("floating: %q %v", a, err)
	}
	if a, err := floatingOf(clients, 11); err != nil || a != "" {
		t.Fatalf("tiled: %q %v", a, err)
	}
	if _, err := floatingOf(clients, 3); err == nil {
		t.Fatal("no window: no error")
	}
}
