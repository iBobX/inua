package answer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// The test program is also the answering one: with ANSWER_TEST set it
// serves, the way inua-assistant --answer does.
func TestMain(m *testing.M) {
	switch os.Getenv("ANSWER_TEST") {
	case "serve":
		profiles := []Profile{{"fast", "model-a"}, {"deep", "model-b"}}
		err := Serve(context.Background(), os.Stdin, os.Stdout, profiles, nil, fake{})
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "off":
		_ = Serve(context.Background(), os.Stdin, os.Stdout, nil, errors.New("The assistant is turned off."), nil)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// fake answers: "deep" chooses the last option itself; the others cannot
// choose, and answer a choice asked as a question with "2".
type fake struct{}

func (fake) Ask(_ context.Context, profile, prompt string) (string, error) {
	switch {
	case prompt == "fail":
		return "", errors.New("no luck")
	case prompt == "slow":
		time.Sleep(300 * time.Millisecond)
	case strings.Contains(prompt, "The options:"):
		return "I pick 2.", nil
	}
	return profile + ":" + strings.ToUpper(prompt), nil
}

func (fake) Choose(_ context.Context, profile, _, _ string, options []string) (int, error) {
	if profile != "deep" {
		return -1, ErrCannotChoose
	}
	return len(options) - 1, nil
}

func start(t *testing.T, mode string) (*Client, error) {
	t.Helper()
	t.Setenv("ANSWER_TEST", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return Start(ctx, exe)
}

func TestAsk(t *testing.T) {
	c, err := start(t, "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.Profiles) != 2 || c.Profiles[1] != (Profile{"deep", "model-b"}) {
		t.Fatalf("profiles %v", c.Profiles)
	}
	ctx := context.Background()
	if a, err := c.Ask(ctx, "", "roll"); err != nil || a != ":ROLL" {
		t.Fatalf("answer %q, %v", a, err)
	}
	if a, err := c.Ask(ctx, "deep", "roll"); err != nil || a != "deep:ROLL" {
		t.Fatalf("answer from deep %q, %v", a, err)
	}
	if _, err := c.Ask(ctx, "", "fail"); err == nil || err.Error() != "no luck" {
		t.Fatalf("error %v", err)
	}
	// A choice: by the profile itself, or asked as a question.
	moves := []string{"roll", "bail", "play a card"}
	if i, err := c.Choose(ctx, "deep", "Pick a move.", "round 1", moves); err != nil || i != 2 {
		t.Fatalf("deep chose %d, %v", i, err)
	}
	if i, err := c.Choose(ctx, "", "Pick a move.", "round 1", moves); err != nil || i != 1 {
		t.Fatalf("the default chose %d, %v", i, err)
	}
	// A question given up on: its late answer is not the next one's.
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	if _, err := c.Ask(short, "", "slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow: %v", err)
	}
	cancel()
	if a, err := c.Ask(ctx, "", "buy"); err != nil || a != ":BUY" {
		t.Fatalf("after the slow one: %q, %v", a, err)
	}
	c.Close()
	if _, err := c.Ask(ctx, "", "again"); err == nil {
		t.Fatal("asked a program that has ended")
	}
}

// The hello says why the AI cannot answer.
func TestChoicePrompt(t *testing.T) {
	p := ChoicePrompt("Pick.", "The state.", []string{"a", "b"})
	if p != "Pick.\n\nThe state.\n\nThe options:\n1. a\n2. b\n\nAnswer with the number of your choice and nothing else." {
		t.Fatalf("prompt %q", p)
	}
	for text, want := range map[string]int{"2": 1, " 1.\n": 0, "Option 2: b": 1, "3": -1, "0": -1, "b": -1, "": -1} {
		if got := ParseChoice(text, 2); got != want {
			t.Errorf("ParseChoice(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestStartFails(t *testing.T) {
	if _, err := start(t, "off"); err == nil || err.Error() != "The assistant is turned off." {
		t.Fatalf("error %v", err)
	}
}
