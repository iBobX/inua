package calc

// Entry is one calculation: the expression as typed and its result.
type Entry struct {
	Expr   string `json:"expr"`
	Result string `json:"result"`
}

// HistoryLen is how many calculations the history keeps.
const HistoryLen = 30

// Remember puts a calculation first in the history, newest first. The
// same expression again moves to the top rather than showing twice; the
// oldest go past HistoryLen.
func Remember(h []Entry, e Entry) []Entry {
	out := []Entry{e}
	for _, old := range h {
		if old.Expr != e.Expr && len(out) < HistoryLen {
			out = append(out, old)
		}
	}
	return out
}

// Recall is the expression after recalling a result: a new expression
// with it when there is none (or an answer is shown), else the result
// added to the one being typed, after an operator or an opening
// parenthesis, or times what is there.
func Recall(text, result string, answered bool) string {
	if answered || text == "" {
		return result
	}
	last := []rune(text)[len([]rune(text))-1]
	for _, r := range "+−×÷^(" {
		if last == r {
			return text + result
		}
	}
	return text + "×" + result
}
