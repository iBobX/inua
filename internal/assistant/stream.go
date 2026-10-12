package assistant

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// answerStream follows a model's streamed reply and yields the answer as it
// grows. The agent's final reply is JSON, {"type":"final","output":"…"},
// so the raw text is not the answer: this finds the output string and
// decodes what has arrived of it, escapes and all. Replies that are not a
// final (a plan, tool calls) yield nothing.
type answerStream struct {
	buf strings.Builder
}

// add appends a delta and returns the answer so far, and whether there is
// one yet.
func (s *answerStream) add(delta string) (string, bool) {
	s.buf.WriteString(delta)
	return partialOutput(s.buf.String())
}

// partialOutput decodes the "output" string of a final reply as far as it
// has arrived.
func partialOutput(text string) (string, bool) {
	if t := strings.Index(text, `"type"`); t >= 0 {
		rest := strings.TrimLeft(text[t+len(`"type"`):], " \t\r\n:")
		if rest != "" && !strings.HasPrefix(rest, `"final"`) && len(rest) >= len(`"final"`) {
			return "", false // a plan or a tool call
		}
	}
	k := strings.Index(text, `"output"`)
	if k < 0 {
		return "", false
	}
	rest := strings.TrimLeft(text[k+len(`"output"`):], " \t\r\n")
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return "", false // not a string (yet)
	}
	return decodePartial(rest[1:]), true
}

// decodePartial decodes a JSON string body up to its closing quote or as
// far as it goes, leaving out an escape cut off at the end.
func decodePartial(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			return b.String()
		case c != '\\':
			_, n := utf8.DecodeRuneInString(s[i:])
			b.WriteString(s[i : i+n])
			i += n
			continue
		case i+1 >= len(s):
			return b.String() // "\" cut off
		}
		switch e := s[i+1]; e {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+6 > len(s) {
				return b.String() // "\u12" cut off
			}
			r, err := strconv.ParseUint(s[i+2:i+6], 16, 32)
			if err != nil {
				return b.String()
			}
			i += 6
			if utf8.ValidRune(rune(r)) && (r < 0xD800 || r > 0xDFFF) {
				b.WriteRune(rune(r))
				continue
			}
			// A surrogate pair: the low half follows.
			if r >= 0xD800 && r < 0xDC00 && i+6 <= len(s) && s[i] == '\\' && s[i+1] == 'u' {
				if lo, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil && lo >= 0xDC00 && lo <= 0xDFFF {
					b.WriteRune(rune((r-0xD800)<<10 + (lo - 0xDC00) + 0x10000))
					i += 6
				}
			}
			continue
		default: // \" \\ \/
			b.WriteByte(e)
		}
		i += 2
	}
	return b.String()
}
