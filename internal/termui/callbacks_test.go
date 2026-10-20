package termui

import (
	"reflect"
	"strings"
	"testing"
)

// Every callback a pane gives GTK is freed with it (callbacks): one left
// out would be lost each time a tile is made, until the process has none.
func TestPaneCallbacksFreed(t *testing.T) {
	var p termPane
	freed := map[uintptr]bool{}
	for _, cb := range p.callbacks() {
		freed[reflect.ValueOf(cb).Pointer()] = true
	}
	v := reflect.ValueOf(&p).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !strings.HasPrefix(f.Name, "on") {
			continue
		}
		field := v.Field(i)
		if field.Kind() == reflect.Array {
			for j := 0; j < field.Len(); j++ {
				if !freed[field.Index(j).Addr().Pointer()] {
					t.Errorf("%s[%d] is not freed", f.Name, j)
				}
			}
			continue
		}
		if !freed[field.Addr().Pointer()] {
			t.Errorf("%s is not freed", f.Name)
		}
	}
}
