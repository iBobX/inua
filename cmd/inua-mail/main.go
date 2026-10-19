// Command inua-mail is Mail (docs/mail.md): the mailboxes of every
// account, their conversations and the conversation chosen, in three
// columns; Gmail and Outlook signed in with their own pages; the
// assistant where it saves time.
//
//	inua-mail [mailto:LINK]        open the window, a composer for a link
//	inua-mail --accounts           the accounts in mail.toml
//	inua-mail --sync ACCOUNT       sign in and sync an account's inbox
//	inua-mail --check FILE.eml     the phishing route on a message
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/mailui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	accounts := flag.Bool("accounts", false, "list the accounts in mail.toml")
	sync := flag.String("sync", "", "sign in to `ACCOUNT` and sync its inbox")
	check := flag.String("check", "", "take the message in `FILE` (.eml) through the phishing checks")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-mail [-ui-dir DIR] [mailto:LINK]")
		fmt.Fprintln(os.Stderr, "       inua-mail --accounts")
		fmt.Fprintln(os.Stderr, "       inua-mail --sync ACCOUNT")
		fmt.Fprintln(os.Stderr, "       inua-mail --check FILE.eml")
		flag.PrintDefaults()
	}
	flag.Parse()
	switch {
	case *accounts:
		os.Exit(listAccounts())
	case *sync != "":
		os.Exit(syncAccount(*sync))
	case *check != "":
		os.Exit(checkFile(*check))
	}
	mailui.Register()
	os.Exit(appui.Run("mail", flag.Args(), *uiDir))
}

func initLang() {
	cfg, _, _ := config.Load(config.DefaultPath())
	i18n.Init(cfg.Language)
}

func listAccounts() int {
	s, err := mail.LoadSettings(mail.SettingsPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(s.Accounts) == 0 {
		fmt.Println("no accounts in", mail.SettingsPath())
		return 0
	}
	for _, a := range s.Accounts {
		i, m := a.Servers()
		fmt.Printf("%-20s %-32s %-10s %s, %s\n", a.ID, a.Address, a.Provider, i, m)
	}
	return 0
}

func syncAccount(id string) int {
	initLang()
	s, err := mail.LoadSettings(mail.SettingsPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	acc := s.Account(id)
	if acc == nil {
		fmt.Fprintf(os.Stderr, "no account %q (inua-mail --accounts lists them)\n", id)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	sess := mail.NewSession(*acc, s, mail.NewStore(), mail.NewSecrets())
	defer sess.Close()
	start := time.Now()
	boxes, err := sess.Mailboxes(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, b := range boxes {
		fmt.Printf("%-8s %-30s %6d unread of %d\n", b.Role, b.Name, b.Unread, b.Total)
	}
	inbox := sess.Role(mail.RoleInbox)
	res, err := sess.Sync(ctx, inbox)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%s: %d new, %d gone, %d kept, in %s\n", inbox, len(res.New), len(res.Gone), len(sess.Items(inbox)), time.Since(start).Round(time.Millisecond))
	return 0
}

func checkFile(path string) int {
	initLang()
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	m, err := mail.Parse(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	j := mail.Judge(m, nil)
	fmt.Printf("%s — %s\n", m.From.String(), m.Subject)
	for _, s := range j.Steps {
		mark := map[mail.Level]string{mail.LevelNone: "□", mail.LevelGood: "■", mail.LevelWarn: "▲", mail.LevelBad: "✕"}[s.Level]
		fmt.Printf("  %s %s %s\n", mark, s.Check, s.Found)
	}
	fmt.Println(j.Verdict.Name())
	if j.Verdict == mail.Suspicious {
		return 3
	}
	return 0
}
