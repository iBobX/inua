package workshop

import "encoding/json"

// Node is a building block of an app's window, as `render` sends it:
// its type, the id its events carry, what it shows, and its children.
// Which fields matter depends on the type (docs/feat/workshop.md).
type Node struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`

	Title       string `json:"title,omitempty"`
	Sub         string `json:"sub,omitempty"`
	Meta        string `json:"meta,omitempty"`
	Text        string `json:"text,omitempty"`
	Label       string `json:"label,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Style       string `json:"style,omitempty"`
	// Value is an input's: a string, a bool for a switch, an option for a
	// drop-down. Applied when the input is made and when it changes from
	// the last render.
	Value      json.RawMessage `json:"value,omitempty"`
	Options    []string        `json:"options,omitempty"`
	Items      [][]string      `json:"items,omitempty"`
	Selected   bool            `json:"selected,omitempty"`
	Password   bool            `json:"password,omitempty"`
	Horizontal bool            `json:"horizontal,omitempty"`
	Scroll     *bool           `json:"scroll,omitempty"`
	Spacing    int             `json:"spacing,omitempty"`
	Width      int             `json:"width,omitempty"`
	Height     int             `json:"height,omitempty"`
	// Min, Max and Step are a slider's range; Min and Max fix a chart's.
	Min    *float64  `json:"min,omitempty"`
	Max    *float64  `json:"max,omitempty"`
	Step   float64   `json:"step,omitempty"`
	Points []float64 `json:"points,omitempty"` // a chart's
	Src    string    `json:"src,omitempty"`    // an image's file

	Children []Node `json:"children,omitempty"`
}

// String is a Value as text; "" when it is not a string.
func (n Node) String() string {
	var s string
	json.Unmarshal(n.Value, &s)
	return s
}

// Bool is a Value as a bool; false when it is not one.
func (n Node) Bool() bool {
	var b bool
	json.Unmarshal(n.Value, &b)
	return b
}

// Number is a Value as a number; 0 when it is not one.
func (n Node) Number() float64 {
	var f float64
	json.Unmarshal(n.Value, &f)
	return f
}

// Types are the building blocks the window draws.
var Types = []string{"page", "columns", "scroll", "box", "section", "row", "text", "facts",
	"button", "entry", "search", "textarea", "switch", "dropdown", "bar", "empty",
	"tabs", "tab", "segmented", "slider", "progress", "spinner", "chart", "tiles", "tile", "image"}
