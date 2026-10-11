package pinentry

import (
	"errors"
	"strings"
	"testing"
)

// session runs Serve over a script of agent lines and returns its replies.
func session(t *testing.T, script string, ask Asker) ([]string, []Request) {
	t.Helper()
	var asked []Request
	var out strings.Builder
	err := Serve(strings.NewReader(script), &out, func(q Request) (Answer, error) {
		asked = append(asked, q)
		return ask(q)
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n"), asked
}

func TestGetPin(t *testing.T) {
	script := "OPTION ttyname=/dev/pts/1\n" +
		"SETDESC Please enter the passphrase to unlock%0Athe key 100%25\n" +
		"SETPROMPT Passphrase:\n" +
		"SETOK _OK\n" +
		"SETERROR Bad Passphrase (try 2 of 3)\n" +
		"GETPIN\n" +
		"GETPIN\n" +
		"BYE\n"
	replies, asked := session(t, script, func(q Request) (Answer, error) {
		return Answer{OK: true, Pin: "pa%ss\nword"}, nil
	})
	want := []string{"OK Pleased", "OK", "OK", "OK", "OK", "OK", "D pa%25ss%0Aword", "OK", "D pa%25ss%0Aword", "OK", "OK closing connection"}
	if len(replies) != len(want) {
		t.Fatalf("replies %q", replies)
	}
	for i, w := range want {
		if !strings.HasPrefix(replies[i], w) {
			t.Errorf("reply %d %q, want %q", i, replies[i], w)
		}
	}
	q := asked[0]
	if q.Mode != GetPin || q.Desc != "Please enter the passphrase to unlock\nthe key 100%" || q.Prompt != "Passphrase:" || q.OK != "OK" || q.Error == "" {
		t.Errorf("request %+v", q)
	}
	if asked[1].Error != "" {
		t.Error("the error was shown twice")
	}
}

func TestCancelAndConfirm(t *testing.T) {
	replies, asked := session(t, "SETREPEAT\nGETPIN\nSETNOTOK _No\nCONFIRM\nCONFIRM --one-button\nGETINFO flavor\nFROB\n",
		func(q Request) (Answer, error) {
			switch q.Mode {
			case Confirm:
				return Answer{NotOK: true}, nil
			case Message:
				return Answer{}, errors.New("no shell")
			}
			return Answer{}, nil // cancelled
		})
	for i, want := range map[int]string{
		2: "ERR 83886179 Operation cancelled",
		4: "ERR 83886194 Not confirmed",
		5: "ERR 83886081 no shell",
		6: "D inua",
		8: "ERR 83886355 Unknown command",
	} {
		if !strings.HasPrefix(replies[i], want) {
			t.Errorf("reply %d %q, want %q", i, replies[i], want)
		}
	}
	if asked[0].Repeat != "Repeat:" || asked[1].NotOK != "No" || asked[2].Mode != Message {
		t.Errorf("asked %+v", asked)
	}
}
