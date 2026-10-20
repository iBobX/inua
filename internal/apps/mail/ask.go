package mail

import (
	"fmt"
	"strings"
	"time"
)

// What the assistant is asked, and how its answers are read
// (docs/feat/inua-mail.md, section 7). Each prompt carries only what its
// use needs; none asks for tools.

// Said is a message as the prompts give it: who, when and the text
// without what it quotes.
type Said struct {
	From Address
	Date time.Time
	Text string
}

// SaidOf is a message for a prompt, its quotes and signature cut.
func SaidOf(m *Message) Said {
	written, _ := Split(m.Text)
	return Said{From: m.From, Date: m.Date, Text: clip(written, 6000)}
}

func conversation(msgs []Said) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "--- %s, %s\n%s\n\n", m.From.String(), m.Date.Format("Mon 2 Jan 2006 15:04"), m.Text)
	}
	return b.String()
}

func langOr(lang string) string {
	if lang == "" {
		return "en"
	}
	return lang
}

// SummaryPrompt asks for a conversation's summary: two or three
// sentences, then what is asked of the user, each on a line of its own
// starting "▸ ".
func SummaryPrompt(msgs []Said, user string, today time.Time, lang string) string {
	return `Summarize this e-mail conversation for its reader, ` + user + `. Today is ` + today.Format("Monday 2 January 2006") + `.

Write two or three plain sentences: what it is about and where it stands now. Then, only if the reader is asked to do something, one line per action starting with "▸ ", with its date or deadline in parentheses when there is one (as "Fri 24 Oct"). No headings, no markdown, no greeting. Write in the language the conversation is written in (its most recent messages when it mixes languages); only if that is unclear, the language whose code is "` + langOr(lang) + `".

The conversation, oldest first:

` + conversation(msgs)
}

// ReplyIdeasPrompt asks for three short replies the newest message calls
// for, each a few words.
func ReplyIdeasPrompt(msgs []Said, user, lang string) string {
	return `You help ` + user + ` answer an e-mail. Give three different short replies the last message calls for, as what the reply would do ("Accept, Friday works", "Ask for the forecast", "Decline politely"). Each at most six words, one per line, nothing else: no numbers, no quotes. In the language of the conversation; if unsure, the language whose code is "` + langOr(lang) + `".

The conversation, oldest first:

` + conversation(msgs)
}

// ParseIdeas reads ReplyIdeasPrompt's answer: up to three lines.
func ParseIdeas(answer string) []string {
	var out []string
	for _, l := range strings.Split(answer, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-*•0123456789.)"))
		l = strings.Trim(l, `"“”'`)
		if l == "" {
			continue
		}
		out = append(out, clip(l, 60))
		if len(out) == 3 {
			break
		}
	}
	return out
}

// DraftPrompt asks for a message's text from a line of what to say, in
// the reply's conversation and the user's voice (examples are a few of
// their sent messages).
func DraftPrompt(what string, msgs []Said, examples []string, user, lang string) string {
	var b strings.Builder
	b.WriteString(`Write the text of an e-mail for ` + user + `. What it should say: ` + what + `

Write only the text: no subject line, no signature, no placeholders in brackets, no markdown. Start with a greeting only if the user's own messages do. Keep it as short as what it says allows. `)
	if len(msgs) > 0 {
		b.WriteString(`It answers the conversation below, in the conversation's language.`)
	} else {
		b.WriteString(`Write in the language of what it should say; if unsure, the language whose code is "` + langOr(lang) + `".`)
	}
	if len(examples) > 0 {
		b.WriteString("\n\nThe user's own recent messages, to match their voice (do not copy them):\n")
		for _, e := range examples {
			fmt.Fprintf(&b, "---\n%s\n", clip(e, 800))
		}
	}
	if len(msgs) > 0 {
		b.WriteString("\n\nThe conversation, oldest first:\n\n" + conversation(msgs))
	}
	return b.String()
}

// Polish is a way of making a text better.
type Polish string

const (
	Shorter    Polish = "shorter"
	Friendlier Polish = "friendlier"
	Formal     Polish = "formal"
	Fix        Polish = "fix"
	Translate  Polish = "translate"
)

// PolishPrompt asks for a text rewritten; to is the language for
// Translate.
func PolishPrompt(text string, how Polish, to string) string {
	ask := map[Polish]string{
		Shorter:    "Make it shorter: keep every fact and request, drop the rest.",
		Friendlier: "Make it warmer and friendlier, without adding facts.",
		Formal:     "Make it more formal and polite, without adding facts.",
		Fix:        "Fix spelling, grammar and punctuation only; change nothing else.",
		Translate:  `Translate it into the language whose code is "` + to + `", keeping its tone.`,
	}[how]
	return `Rewrite the text of this e-mail. ` + ask + ` Keep it in its language unless asked to translate. Answer with the new text only: no comments, no quotes, no markdown.

The text:

` + text
}

// SearchPrompt asks for a sentence turned into the search's operators.
func SearchPrompt(sentence string, today time.Time) string {
	return `Turn a request for an e-mail into a search query. Today is ` + today.Format("2006-01-02 (Monday)") + `.

Use only these operators, separated by spaces: from:WORD to:WORD subject:WORD has:attachment is:unread is:starred after:YYYY-MM-DD before:YYYY-MM-DD, and plain words to find in the text (a phrase in double quotes). Prefer a name or domain word for from: (from:hetzner). Use few words: the ones that must appear. Answer with the query on one line and nothing else.

The request: ` + sentence
}

// ParseSearch reads SearchPrompt's answer: its first line, without
// backticks.
func ParseSearch(answer string) string {
	for _, l := range strings.Split(answer, "\n") {
		l = strings.Trim(strings.TrimSpace(l), "`")
		l = strings.TrimSpace(strings.TrimPrefix(l, "query:"))
		if l != "" {
			return l
		}
	}
	return ""
}

// JudgePrompt asks the assistant whether a message is phishing, after
// the checks; the answer is a word and a sentence.
func JudgePrompt(facts, lang string) string {
	return `Is this e-mail a phishing or scam attempt? The checks inua already did are listed with what they found.

Answer in exactly this form, nothing before or after it, the reasons and the advice in the language whose code is "` + langOr(lang) + `":

VERDICT: FINE, CAREFUL or SUSPICIOUS (FINE: an ordinary message; CAREFUL: something is off, check before acting; SUSPICIOUS: signs of phishing or fraud)
- a reason, one short line
- another reason, if there is one (three at most)
DO: one short line on what the reader should do

` + facts
}

// AIJudgment is the assistant's answer to JudgePrompt: its verdict, its
// reasons and what it advises.
type AIJudgment struct {
	Verdict Verdict
	Reasons []string
	Advice  string
}

// ParseJudgment reads JudgePrompt's answer; an answer in another form
// still gives its verdict when its first words name one, and its other
// lines as reasons.
func ParseJudgment(answer string) (AIJudgment, bool) {
	var j AIJudgment
	found := false
	for _, line := range strings.Split(strings.TrimSpace(answer), "\n") {
		line = strings.TrimSpace(line)
		plain := strings.ToUpper(strings.Trim(line, "*_` "))
		switch {
		case line == "":
		case !found && verdictWord(plain) >= 0:
			j.Verdict, found = Verdict(verdictWord(plain)), true
		case strings.HasPrefix(plain, "DO:"):
			j.Advice = strings.TrimSpace(strings.Trim(line, "*_` ")[3:])
		case strings.HasPrefix(line, "-") || strings.HasPrefix(line, "•") || strings.HasPrefix(line, "*"):
			if r := strings.TrimSpace(strings.TrimLeft(line, "-•* ")); r != "" {
				j.Reasons = append(j.Reasons, r)
			}
		case found:
			j.Reasons = append(j.Reasons, line)
		}
	}
	if len(j.Reasons) > 3 {
		j.Reasons = j.Reasons[:3]
	}
	return j, found
}

// verdictWord is the verdict a line names first ("VERDICT: SUSPICIOUS",
// "**Suspicious**"), or -1.
func verdictWord(line string) int {
	line = strings.TrimSpace(strings.TrimPrefix(line, "VERDICT:"))
	switch {
	case strings.HasPrefix(line, "FINE"):
		return int(Fine)
	case strings.HasPrefix(line, "CAREFUL"):
		return int(Careful)
	case strings.HasPrefix(line, "SUSPICIOUS"):
		return int(Suspicious)
	}
	return -1
}
