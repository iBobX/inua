package i18n

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestWanted(t *testing.T) {
	for _, c := range []struct {
		language, all, messages, lang string
		want                          string
	}{
		{"", "", "", "zh_CN.UTF-8", "zh_CN zh"},
		{"", "de_DE.UTF-8@euro", "", "zh_CN.UTF-8", "de_DE de"},
		{"", "", "ja_JP.UTF-8", "en_US.UTF-8", "ja_JP ja"},
		{"fr:zh_TW", "", "", "en_US.UTF-8", "fr zh_TW zh en_US en"},
		{"", "C", "", "", ""},
	} {
		t.Setenv("LANGUAGE", c.language)
		t.Setenv("LC_ALL", c.all)
		t.Setenv("LC_MESSAGES", c.messages)
		t.Setenv("LANG", c.lang)
		if got := strings.Join(Wanted(), " "); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestTranslate(t *testing.T) {
	defer Use() // English again for the other tests
	if Use("de", "en") != "" || T("Pause") != "Pause" {
		t.Fatal("English is not the source")
	}
	if Use("xx", "zh") != "" { // a language alone, without its catalog
		t.Error("loaded zh, which has no catalog of its own")
	}
	if Use("xx", "zh_CN") != "zh_CN" || Lang() != "zh_CN" {
		t.Fatalf("zh_CN not loaded: %q", Lang())
	}
	if T("Pause") != "暂停" || T("Not a message") != "Not a message" {
		t.Errorf("T: %q %q", T("Pause"), T("Not a message"))
	}
	if got := Tf("Process %d has ended.", 42); got != "进程 42 已结束。" {
		t.Errorf("Tf: %q", got)
	}
	// One plural form in Chinese; an argument taken out of order.
	if got := Nf("%d core", "%d cores", 8, 8); got != "8 个核心" {
		t.Errorf("Nf: %q", got)
	}
	if got := Tf("All %d processes named %s.", 3, "sh"); got != "名为 sh 的全部 3 个进程。" {
		t.Errorf("Tf, reordered: %q", got)
	}
	Use()
	if Nf("%d core", "%d cores", 1, 1) != "1 core" || Nf("%d core", "%d cores", 2, 2) != "2 cores" {
		t.Error("English plurals")
	}
}

func TestUI(t *testing.T) {
	defer Use()
	ui := `<object class="GtkLabel"><property name="label" translatable="yes">Pause</property>` +
		`<property name="tooltip-text" translatable="yes">Close (Escape)</property>` +
		`<property name="name">Pause</property>` +
		`<property name="label" translatable="yes">Tom &amp; Jerry</property></object>`
	if UI(ui) != ui {
		t.Error("changed in English")
	}
	Use("zh_CN")
	got := UI(ui)
	for _, want := range []string{`translatable="yes">暂停<`, `>关闭（Esc）<`, `<property name="name">Pause</property>`, `>Tom &amp; Jerry<`} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %s", want, got)
		}
	}
}

func TestParsePO(t *testing.T) {
	po := "# a comment\nmsgid \"\"\nmsgstr \"\"\n\"Plural-Forms: nplurals=2; plural=(n != 1);\\n\"\n\n" +
		"#, fuzzy\nmsgid \"Old\"\nmsgstr \"Vieux\"\n\n" +
		"msgctxt \"menu\"\nmsgid \"Open\"\nmsgstr \"Ouvrir\"\n\n" +
		"msgid \"a long \"\n\"line\"\nmsgstr \"une longue \"\n\"ligne\\n\"\n\n" +
		"msgid \"%d file\"\nmsgid_plural \"%d files\"\nmsgstr[0] \"%d fichier\"\nmsgstr[1] \"%d fichiers\"\n"
	c, err := parse(po)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.msgs["Old"]; ok {
		t.Error("used a fuzzy translation")
	}
	if c.msgs["menu\x04Open"][0] != "Ouvrir" || c.msgs["a long line"][0] != "une longue ligne\n" {
		t.Errorf("%q", c.msgs)
	}
	if f := c.msgs["%d file"]; len(f) != 2 || f[c.plural(1)] != "%d fichier" || f[c.plural(5)] != "%d fichiers" {
		t.Errorf("plurals %q", f)
	}
	if _, err := ParsePO("msgid \"x\"\nmsgstr nope\n"); err == nil {
		t.Error("read a broken file")
	}
}

// Every catalog reads, and each translation takes its message's
// arguments: the same verbs, so none is dropped or misread.
func TestCatalogs(t *testing.T) {
	verb := regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*[\d.]*[a-zA-Z%]`)
	verbs := func(s string) []string {
		var out []string
		for _, v := range verb.FindAllString(s, -1) {
			out = append(out, regexp.MustCompile(`\[\d+\]`).ReplaceAllString(v, ""))
		}
		slices.Sort(out)
		return out
	}
	files, _ := fs.Glob(locales, "locales/*.po")
	if len(files) == 0 {
		t.Fatal("no catalogs")
	}
	for _, f := range files {
		data, _ := locales.ReadFile(f)
		entries, err := ParsePO(string(data))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		for _, e := range entries {
			if e.ID == "" {
				continue
			}
			for i, s := range e.Str {
				src := e.ID
				if i > 0 && e.Plural != "" {
					src = e.Plural
				}
				if s != "" && !slices.Equal(verbs(s), verbs(src)) {
					t.Errorf("%s: %q: %q takes other arguments", f, e.ID, s)
				}
			}
		}
	}
}
