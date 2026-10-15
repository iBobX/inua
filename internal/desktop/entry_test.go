package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const firefox = `[Desktop Entry]
Type=Application
Name=Firefox
Name[zh_CN]=火狐浏览器
GenericName=Web Browser
Comment=Browse the Web
Icon=firefox
Exec=firefox %u
Categories=Network;WebBrowser;
Keywords=web;internet;
Keywords[zh]=网页;互联网;

[Desktop Action new-window]
Name=New Window
Exec=firefox --new-window
`

func TestParseLocalized(t *testing.T) {
	e, err := Parse(strings.NewReader(firefox), "firefox", "zh_CN.UTF-8")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "火狐浏览器" || e.GenericName != "Web Browser" {
		t.Errorf("names: %q %q", e.Name, e.GenericName)
	}
	if !slices.Equal(e.Keywords, []string{"网页", "互联网"}) {
		t.Errorf("keywords fall back to zh: %q", e.Keywords)
	}
	if !slices.Equal(e.Command, []string{"firefox"}) {
		t.Errorf("action group leaked or %%u kept: %q", e.Command)
	}
	if !slices.Equal(e.Categories, []string{"Network", "WebBrowser"}) {
		t.Errorf("categories %q", e.Categories)
	}

	e, _ = Parse(strings.NewReader(firefox), "firefox", "")
	if e.Name != "Firefox" {
		t.Errorf("untranslated name %q", e.Name)
	}
}

func TestParseRejects(t *testing.T) {
	for name, src := range map[string]string{
		"link":   "[Desktop Entry]\nType=Link\nName=x\n",
		"hidden": "[Desktop Entry]\nType=Application\nName=x\nHidden=true\nExec=x\n",
		"noname": "[Desktop Entry]\nType=Application\nExec=x\n",
	} {
		if _, err := Parse(strings.NewReader(src), name, ""); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestParseExec(t *testing.T) {
	e := &Entry{Name: "Ed", Icon: "ed", Path: "/a/ed.desktop"}
	cases := map[string][]string{
		`ed %F`:                       {"ed"},
		`"/opt/My App/run" --flag %U`: {"/opt/My App/run", "--flag"},
		`sh -c "echo \"hi\" \\$HOME"`: {"sh", "-c", `echo "hi" \$HOME`},
		`ed %i --title=%c --file=%k`:  {"ed", "--icon", "ed", "--title=Ed", "--file=/a/ed.desktop"},
		`printf 100%%`:                {"printf", "100%"},
	}
	for in, want := range cases {
		got, err := ParseExec(in, e)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ParseExec(%s) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseExec(`ed "unterminated`, e); err == nil {
		t.Error("unterminated quote accepted")
	}
}

func TestScanPrecedence(t *testing.T) {
	user, system := t.TempDir(), t.TempDir()
	write := func(dir, rel, body string) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := func(name string) string { return "[Desktop Entry]\nType=Application\nName=" + name + "\nExec=x\n" }
	write(user, "a.desktop", app("A user"))
	write(user, "gone.desktop", "[Desktop Entry]\nType=Application\nName=g\nHidden=true\n")
	write(system, "a.desktop", app("A system"))
	write(system, "gone.desktop", app("should be hidden"))
	write(system, "kde/b.desktop", app("B"))

	got := Scan([]string{user, system}, "")
	var ids, names []string
	for _, e := range got {
		ids = append(ids, e.ID)
		names = append(names, e.Name)
	}
	if !slices.Equal(ids, []string{"a", "kde-b"}) || names[0] != "A user" {
		t.Fatalf("ids %q names %q", ids, names)
	}
}

// Stamp changes when an entry is added to one of the folders, not
// otherwise.
func TestStamp(t *testing.T) {
	a, b := t.TempDir(), filepath.Join(t.TempDir(), "missing")
	before := Stamp([]string{a, b})
	if Stamp([]string{a, b}) != before {
		t.Error("the stamp changed with nothing done")
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(a, "new.desktop"), []byte("[Desktop Entry]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Stamp([]string{a, b}) == before {
		t.Error("the stamp stayed after an entry was added")
	}
}

func TestExecFiles(t *testing.T) {
	e := &Entry{Name: "Editor", Icon: "ed"}
	paths := []string{"/home/u/a b.txt", "/tmp/c.txt"}
	cases := []struct {
		exec string
		want [][]string
	}{
		{"editor %F", [][]string{{"editor", "/home/u/a b.txt", "/tmp/c.txt"}}},
		{"browser %U", [][]string{{"browser", "file:///home/u/a%20b.txt", "file:///tmp/c.txt"}}},
		{"viewer --one %f", [][]string{{"viewer", "--one", "/home/u/a b.txt"}, {"viewer", "--one", "/tmp/c.txt"}}},
		{"plain -x", [][]string{{"plain", "-x", "/home/u/a b.txt", "/tmp/c.txt"}}},
		{`"my app" %i %u`, [][]string{{"my app", "--icon", "ed", "file:///home/u/a%20b.txt"}, {"my app", "--icon", "ed", "file:///tmp/c.txt"}}},
	}
	for _, c := range cases {
		got, err := ExecFiles(c.exec, e, paths)
		if err != nil {
			t.Fatalf("%s: %v", c.exec, err)
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.exec, got, c.want)
		}
	}
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "kde"), 0o755)
	os.WriteFile(filepath.Join(dir, "org.Ed.desktop"), []byte("[Desktop Entry]\nType=Application\nName=Ed\nExec=ed %F\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "kde", "view.desktop"), []byte("[Desktop Entry]\nType=Application\nName=View\nExec=view %u\n"), 0o644)
	e, err := Find([]string{t.TempDir(), dir}, "org.Ed.desktop", "")
	if err != nil || e.Name != "Ed" || e.ExecString != "ed %F" || e.Path != filepath.Join(dir, "org.Ed.desktop") {
		t.Fatalf("%+v %v", e, err)
	}
	if e, err := Find([]string{dir}, "kde-view", ""); err != nil || e.Name != "View" {
		t.Fatalf("subfolder: %+v %v", e, err)
	}
	if _, err := Find([]string{dir}, "nothing", ""); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}
