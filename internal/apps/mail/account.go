// Package mail is inua-mail's logic (docs/feat/inua-mail.md): the accounts
// and their sign-in, keeping a mailbox in step with its server, reading and
// building messages, conversations, search, the phishing route and what is
// asked of the assistant. It does not import GTK; internal/mailui is the
// window.
package mail

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/lyricat/inua/internal/i18n"
)

// Provider is how an account signs in and where its servers are.
type Provider string

const (
	Google    Provider = "google"    // OAuth; imap.gmail.com
	Microsoft Provider = "microsoft" // OAuth; outlook.office365.com
	Password  Provider = "imap"      // any IMAP and SMTP server, with a password
)

// Account is one mailbox the user reads; nothing secret.
type Account struct {
	ID        string   `toml:"id"`
	Name      string   `toml:"name"`    // the name on what is sent
	Address   string   `toml:"address"` // the address it sends from
	Provider  Provider `toml:"provider"`
	Signature string   `toml:"signature,omitempty"`
	Picture   string   `toml:"picture,omitempty"`  // the provider's photo of the user, an address
	IMAP      string   `toml:"imap,omitempty"`     // host:port; 993 TLS, else STARTTLS
	SMTP      string   `toml:"smtp,omitempty"`     // host:port; 465 TLS, else STARTTLS
	Username  string   `toml:"username,omitempty"` // the address when empty
}

// OAuthClient is an app registered with a provider: the user's own, or
// inua's when left out.
type OAuthClient struct {
	ClientID string `toml:"client_id,omitempty"`
	// ClientSecret is a secret written here by hand; the welcome keeps the
	// one typed in it in the keyring instead (SecretInKeyring).
	ClientSecret    string `toml:"client_secret,omitempty"`
	SecretInKeyring bool   `toml:"secret_in_keyring,omitempty"`
	Tenant          string `toml:"tenant,omitempty"` // Microsoft: "common" when empty
}

// Settings is mail.toml.
type Settings struct {
	// Keyring holds the user's own clients' secrets (SecretInKeyring).
	Keyring  *Secrets  `toml:"-"`
	Accounts []Account `toml:"account"`
	OAuth    struct {
		Google    OAuthClient `toml:"google"`
		Microsoft OAuthClient `toml:"microsoft"`
	} `toml:"oauth"`
	Assistant struct {
		// Off turns every AI feature of Mail off: the assistant's panel,
		// Ask AI, search from a sentence, drafting and polishing, an
		// attachment's summary, and sorting.
		Off bool `toml:"off,omitempty"`
		// Sort tags new mail in the inbox, asking the assistant when the
		// rules do not settle it (docs/feat/inua-mail.md, 7.2).
		Sort bool `toml:"sort"`
	} `toml:"assistant"`
	// Window is the window's layout as it was left.
	Window struct {
		HideSidebar bool `toml:"hide_sidebar,omitempty"`
		Assistant   bool `toml:"assistant,omitempty"` // the right panel is open
		// PanelHid says the panel hid the sidebar, to make room: it comes
		// back when the panel closes.
		PanelHid bool `toml:"panel_hid_sidebar,omitempty"`
	} `toml:"window"`
	Reading Reading `toml:"reading"`
	Writing struct {
		// UndoSend is the seconds a send waits, to be taken back: 0 is 5,
		// below 0 none.
		UndoSend int `toml:"undo_send,omitempty"`
	} `toml:"writing"`
}

// Reading is how messages are shown.
type Reading struct {
	// Text shows HTML mail as plain text; else formatted (the default).
	Text bool `toml:"text,omitempty"`
	// Images is what to do with an HTML message's images from the web:
	// "ask" (the default: a line offers to load them), "always" or "never".
	Images string `toml:"images,omitempty"`
	// ImagesFrom are the senders whose images load without asking.
	ImagesFrom []string `toml:"images_from,omitempty"`
	// KeepUnread leaves a conversation unread when it is opened.
	KeepUnread bool `toml:"keep_unread,omitempty"`
}

// AI says whether Mail's AI features are on.
func (s Settings) AI() bool { return !s.Assistant.Off }

// LoadImages says whether a sender's images from the web load at once.
func (r Reading) LoadImages(from string) bool {
	switch r.Images {
	case "always":
		return true
	case "never":
		return false
	}
	return slices.ContainsFunc(r.ImagesFrom, func(a string) bool { return strings.EqualFold(a, from) })
}

// UndoSeconds is how long a send waits to be taken back.
func (s Settings) UndoSeconds() int {
	switch {
	case s.Writing.UndoSend < 0:
		return 0
	case s.Writing.UndoSend == 0:
		return 5
	}
	return s.Writing.UndoSend
}

// SettingsPath is ~/.config/inua/mail.toml.
func SettingsPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "inua", "mail.toml")
}

// LoadSettings reads the file; none is no accounts.
func LoadSettings(path string) (Settings, error) {
	var s Settings
	_, err := toml.DecodeFile(path, &s)
	if errors.Is(err, fs.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	for i := range s.Accounts {
		if err := s.Accounts[i].check(); err != nil {
			return s, fmt.Errorf("%s: %w", path, err)
		}
	}
	return s, nil
}

// Save writes the file.
func (s Settings) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mail-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	fmt.Fprintln(f, "# inua-mail's accounts (docs/mail.md). Passwords and tokens are in the keyring.")
	if err := toml.NewEncoder(f).Encode(s); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Account finds an account by ID.
func (s *Settings) Account(id string) *Account {
	for i := range s.Accounts {
		if s.Accounts[i].ID == id {
			return &s.Accounts[i]
		}
	}
	return nil
}

// Add puts a in the settings with an ID of its own, or replaces the
// account with the same address and provider (signing in again).
func (s *Settings) Add(a Account) Account {
	for i, b := range s.Accounts {
		if strings.EqualFold(b.Address, a.Address) && b.Provider == a.Provider {
			a.ID = b.ID
			if a.Signature == "" {
				a.Signature = b.Signature
			}
			if a.Name == "" {
				a.Name = b.Name
			}
			if a.Picture == "" {
				a.Picture = b.Picture
			}
			s.Accounts[i] = a
			return a
		}
	}
	a.ID = s.newID(a.Address)
	s.Accounts = append(s.Accounts, a)
	return a
}

// Remove takes the account with this ID out.
func (s *Settings) Remove(id string) {
	s.Accounts = slices.DeleteFunc(s.Accounts, func(a Account) bool { return a.ID == id })
}

var idChars = regexp.MustCompile(`[^a-z0-9]+`)

// newID makes an ID from an address: its part before the @ and its
// domain's first name, made unique.
func (s *Settings) newID(address string) string {
	local, domain, _ := strings.Cut(strings.ToLower(address), "@")
	domain, _, _ = strings.Cut(domain, ".")
	base := strings.Trim(idChars.ReplaceAllString(local+"-"+domain, "-"), "-")
	if base == "" {
		base = "account"
	}
	id := base
	for n := 2; s.Account(id) != nil; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

func (a *Account) check() error {
	switch a.Provider {
	case Google, Microsoft:
	case Password:
		if a.IMAP == "" || a.SMTP == "" {
			return fmt.Errorf("account %q: imap and smtp are needed", a.ID)
		}
	case "":
		a.Provider = Password
		return a.check()
	default:
		return fmt.Errorf("account %q: provider %q is not google, microsoft or imap", a.ID, a.Provider)
	}
	if a.ID == "" || a.Address == "" {
		return errors.New("an account needs an id and an address")
	}
	return nil
}

// Servers are the account's IMAP and SMTP servers, host:port.
func (a Account) Servers() (imapAddr, smtpAddr string) {
	switch a.Provider {
	case Google:
		return "imap.gmail.com:993", "smtp.gmail.com:465"
	case Microsoft:
		return "outlook.office365.com:993", "smtp.office365.com:587"
	}
	return withPort(a.IMAP, "993"), withPort(a.SMTP, "465")
}

func withPort(addr, port string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil || addr == "" {
		return addr
	}
	return net.JoinHostPort(addr, port)
}

// User is the name the account signs in with.
func (a Account) User() string {
	if a.Username != "" {
		return a.Username
	}
	return a.Address
}

// OAuth says whether the account signs in with a provider's page.
func (a Account) OAuth() bool { return a.Provider == Google || a.Provider == Microsoft }

// SavesSent says whether the server puts what its SMTP sends in Sent
// itself, so inua must not append it again.
func (a Account) SavesSent() bool { return a.OAuth() }

// Name is the provider in words.
func (p Provider) Name() string {
	switch p {
	case Google:
		return "Google"
	case Microsoft:
		return "Microsoft"
	}
	return i18n.T("IMAP")
}

// Detect says how an address signs in: Google and Microsoft for their
// mail domains, IMAP for the rest (a company's Google Workspace or
// Microsoft 365 domain cannot be told from its name; the user picks it).
func Detect(address string) Provider {
	_, domain, _ := strings.Cut(strings.ToLower(strings.TrimSpace(address)), "@")
	switch {
	case domain == "gmail.com" || domain == "googlemail.com":
		return Google
	case domain == "outlook.com" || domain == "msn.com" || domain == "passport.com" ||
		strings.HasPrefix(domain, "hotmail.") || strings.HasPrefix(domain, "live.") || strings.HasPrefix(domain, "outlook."):
		return Microsoft
	}
	return Password
}

// Guess fills in the servers of a password account from its address,
// for the providers inua knows; false when it does not know them.
func Guess(address string) (imapAddr, smtpAddr string, ok bool) {
	_, domain, _ := strings.Cut(strings.ToLower(strings.TrimSpace(address)), "@")
	for _, k := range known {
		if slices.Contains(k.domains, domain) {
			return k.imap, k.smtp, true
		}
	}
	return "", "", false
}

var known = []struct {
	domains    []string
	imap, smtp string
}{
	{[]string{"fastmail.com", "fastmail.fm"}, "imap.fastmail.com:993", "smtp.fastmail.com:465"},
	{[]string{"icloud.com", "me.com", "mac.com"}, "imap.mail.me.com:993", "smtp.mail.me.com:587"},
	{[]string{"yahoo.com", "ymail.com"}, "imap.mail.yahoo.com:993", "smtp.mail.yahoo.com:465"},
	{[]string{"proton.me", "protonmail.com", "pm.me"}, "127.0.0.1:1143", "127.0.0.1:1025"}, // Proton Mail Bridge
	{[]string{"zoho.com"}, "imap.zoho.com:993", "smtp.zoho.com:465"},
	{[]string{"gmx.com", "gmx.net", "gmx.de"}, "imap.gmx.com:993", "mail.gmx.com:465"},
	{[]string{"aol.com"}, "imap.aol.com:993", "smtp.aol.com:465"},
	{[]string{"qq.com"}, "imap.qq.com:993", "smtp.qq.com:465"},
	{[]string{"163.com"}, "imap.163.com:993", "smtp.163.com:465"},
}
