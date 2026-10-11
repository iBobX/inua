package colors

import (
	"fmt"
	"testing"
)

func TestForms(t *testing.T) {
	c, ok := Parse("#2463AE")
	if !ok || c.Hex() != "#2463ae" || c.RGB() != "rgb(36, 99, 174)" || c.HSL() != "hsl(213, 66%, 41%)" {
		t.Errorf("%v %s %s %s", ok, c.Hex(), c.RGB(), c.HSL())
	}
	if g, _ := Parse("808080"); g.HSL() != "hsl(0, 0%, 50%)" {
		t.Error(g.HSL())
	}
	if _, ok := Parse("#12345"); ok {
		t.Error("five digits")
	}
	if w, _ := Parse("#ffffff"); !w.Light() {
		t.Error("white is light")
	}
	if k, _ := Parse("#101010"); k.Light() {
		t.Error("near black is dark")
	}
}

func TestHistory(t *testing.T) {
	var d Data
	for i := range Keep + 5 {
		d.Add(fmt.Sprintf("#0000%02x", i))
	}
	d.Add("#000000") // already there: moves to the front
	if len(d.History) != Keep || d.History[0] != "#000000" || d.History[1] != fmt.Sprintf("#0000%02x", Keep+4) {
		t.Errorf("%d %v", len(d.History), d.History[:3])
	}
}
