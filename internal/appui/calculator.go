package appui

import (
	"strings"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/calc"
)

// calcApp is the calculator. The expression is kept as shown (× ÷ −);
// the result below previews it while it is typed and becomes the answer
// on =. After =, a number starts a new expression and an operator goes on
// from the answer.
type calcApp struct {
	expr, result     gtk.Label
	sci, deg         gtk.ToggleButton
	scikeys, keys    gtk.Grid
	text             string
	answered         bool
	answer           string
	onKey            func(gtk.Button)
	onKeyPress       func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onSci, onDegrees func(gtk.ToggleButton)
}

// The keys: label, input, style. Inputs starting with "op:" are commands.
var calcKeys = [][]struct{ label, input, style string }{
	{{"AC", "op:clear", "fn"}, {"⌫", "op:back", "fn"}, {"%", "%", "fn"}, {"÷", "÷", "op"}},
	{{"7", "7", ""}, {"8", "8", ""}, {"9", "9", ""}, {"×", "×", "op"}},
	{{"4", "4", ""}, {"5", "5", ""}, {"6", "6", ""}, {"−", "−", "op"}},
	{{"1", "1", ""}, {"2", "2", ""}, {"3", "3", ""}, {"+", "+", "op"}},
	{{"±", "op:negate", ""}, {"0", "0", ""}, {".", ".", ""}, {"=", "op:equals", "eq"}},
}

var calcSciKeys = [][]struct{ label, input string }{
	{{"(", "("}, {")", ")"}, {"xʸ", "^"}},
	{{"sin", "sin("}, {"cos", "cos("}, {"tan", "tan("}},
	{{"ln", "ln("}, {"log", "log("}, {"√", "√("}},
	{{"x²", "^2"}, {"π", "π"}, {"e", "e"}},
	{{"sin⁻¹", "asin("}, {"cos⁻¹", "acos("}, {"tan⁻¹", "atan("}},
}

func (a *calcApp) build(c *Ctx) *gtk.ApplicationWindow {
	win := &gtk.ApplicationWindow{}
	ui := c.Load("calculator.ui")
	ui.Get("calculator", win)
	ui.Get("expression", &a.expr)
	ui.Get("result", &a.result)
	ui.Get("sci", &a.sci)
	ui.Get("deg", &a.deg)
	ui.Get("scikeys", &a.scikeys)
	ui.Get("keys", &a.keys)
	ui.Close()

	a.onKey = func(b gtk.Button) { a.input(b.GetName()) }
	a.onSci = func(b gtk.ToggleButton) { a.scikeys.SetVisible(b.GetActive()) }
	a.onDegrees = func(gtk.ToggleButton) { a.show() }
	a.onKeyPress = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		return a.typed(keyval, state)
	}
	key := func(grid *gtk.Grid, label, input, style string, col, row int) {
		b := gtk.NewButtonWithLabel(label)
		b.AddCssClass("calc-key")
		if style != "" {
			b.AddCssClass(style)
		}
		b.SetName(input)
		b.SetFocusOnClick(false)
		b.ConnectClicked(&a.onKey)
		grid.Attach(&b.Widget, int32(col), int32(row), 1, 1)
		b.Unref() // the grid holds it
	}
	for r, row := range calcKeys {
		for col, k := range row {
			key(&a.keys, k.label, k.input, k.style, col, r)
		}
	}
	for r, row := range calcSciKeys {
		for col, k := range row {
			key(&a.scikeys, k.label, k.input, "sci", col, r)
		}
	}
	a.sci.ConnectToggled(&a.onSci)
	a.deg.ConnectToggled(&a.onDegrees)
	a.sci.SetCanFocus(false)
	a.deg.SetCanFocus(false)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue) // before a focused button takes Enter
	keys.ConnectKeyPressed(&a.onKeyPress)
	win.AddController(&keys.EventController)
	a.show()
	return win
}

func (a *calcApp) themeChanged() {}

// typed turns a key press into input.
func (a *calcApp) typed(keyval uint32, state gdk.ModifierType) bool {
	if state&gdk.ControlMaskValue != 0 {
		return false
	}
	switch keyval {
	case 0xff0d, 0xff8d: // Return, KP_Enter
		a.input("op:equals")
		return true
	case 0xff08: // BackSpace
		a.input("op:back")
		return true
	case 0xff1b, 0xffff: // Escape, Delete
		a.input("op:clear")
		return true
	}
	r := rune(gdk.KeyvalToUnicode(keyval))
	in := map[rune]string{'*': "×", 'x': "×", '/': "÷", '-': "−", '=': "op:equals"}[r]
	if in == "" && r != 0 && strings.ContainsRune("0123456789.+%^()", r) {
		in = string(r)
	}
	if in == "" {
		return false
	}
	a.input(in)
	return true
}

// input takes one key's input.
func (a *calcApp) input(in string) {
	switch in {
	case "op:clear":
		a.text, a.answered = "", false
	case "op:back":
		if a.answered {
			a.text, a.answered = "", false
			break
		}
		a.text = trimLast(a.text)
	case "op:negate":
		switch {
		case a.answered:
			a.text, a.answered = "−("+a.answer+")", false
		case strings.HasPrefix(a.text, "−(") && strings.HasSuffix(a.text, ")"):
			a.text = a.text[len("−(") : len(a.text)-1]
		case a.text != "":
			a.text = "−(" + a.text + ")"
		}
	case "op:equals":
		v, err := calc.Eval(a.text, a.deg.GetActive())
		if a.text == "" || err != nil {
			a.show()
			if a.text != "" {
				a.result.SetText("Error")
				a.result.RemoveCssClass("preview")
			}
			return
		}
		a.answer = calc.Format(v)
		a.expr.SetText(a.text + " =")
		a.result.SetText(a.answer)
		a.result.RemoveCssClass("preview")
		a.answered = true
		return
	default:
		if a.answered {
			if r, _ := utf8.DecodeRuneInString(in); strings.ContainsRune("+−×÷^%", r) {
				a.text = a.answer // go on from the answer
			} else {
				a.text = ""
			}
			a.answered = false
		}
		a.text += in
	}
	a.show()
}

// trimLast removes the last thing typed: a function name with its
// parenthesis counts as one.
func trimLast(s string) string {
	for _, f := range []string{"asin(", "acos(", "atan(", "sin(", "cos(", "tan(", "log(", "ln(", "√("} {
		if strings.HasSuffix(s, f) {
			return s[:len(s)-len(f)]
		}
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

// show puts the expression up, with its value as a preview.
func (a *calcApp) show() {
	if a.answered {
		return
	}
	a.expr.SetText(a.text)
	a.result.AddCssClass("preview")
	if a.text == "" {
		a.result.SetText("0")
		return
	}
	if v, err := calc.Eval(a.text, a.deg.GetActive()); err == nil {
		a.result.SetText(calc.Format(v))
	} else {
		a.result.SetText("")
	}
}
