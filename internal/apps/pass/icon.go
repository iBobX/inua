package pass

import (
	"slices"
	"strings"
	"unicode"
)

// brands are services whose logo Phosphor draws, by a word of an entry's
// name: "github" of "work/github.com" or "personal/github/ann".
var brands = map[string]string{
	"github": "github-logo", "gitlab": "gitlab-logo",
	"google": "google-logo", "gmail": "google-logo", "youtube": "youtube-logo",
	"apple": "apple-logo", "icloud": "apple-logo", "appleid": "apple-logo",
	"amazon": "amazon-logo", "discord": "discord-logo", "slack": "slack-logo",
	"twitter": "x-logo", "facebook": "facebook-logo", "instagram": "instagram-logo",
	"linkedin": "linkedin-logo", "reddit": "reddit-logo", "spotify": "spotify-logo",
	"telegram": "telegram-logo", "whatsapp": "whatsapp-logo", "twitch": "twitch-logo",
	"paypal": "paypal-logo", "dropbox": "dropbox-logo", "figma": "figma-logo",
	"notion": "notion-logo", "steam": "steam-logo", "stripe": "stripe-logo",
	"mastodon": "mastodon-logo", "microsoft": "windows-logo", "windows": "windows-logo",
	"outlook": "microsoft-outlook-logo", "hotmail": "microsoft-outlook-logo",
	"teams": "microsoft-teams-logo", "android": "android-logo",
	"pinterest": "pinterest-logo", "tiktok": "tiktok-logo",
}

// kinds are what an entry is for, by a word of its own name or its
// folder's, in order: the first that matches.
var kinds = []struct {
	icon  string
	words []string
}{
	{"terminal-window", []string{"ssh", "shell", "sftp"}},
	{"database", []string{"db", "database", "postgres", "postgresql", "mysql", "mariadb", "redis", "mongo", "mongodb", "sqlite", "elastic"}},
	{"hard-drives", []string{"server", "servers", "vps", "host", "nas", "prod", "staging", "kubernetes", "k8s"}},
	{"cloud", []string{"cloud", "aws", "gcp", "azure", "s3", "digitalocean", "cloudflare", "hetzner", "linode", "vercel", "netlify", "heroku"}},
	{"wifi-high", []string{"wifi", "wlan", "ssid", "wireless", "hotspot"}},
	{"shield-check", []string{"vpn", "wireguard", "openvpn", "tailscale", "zerotier"}},
	{"network", []string{"router", "modem", "switch", "lan", "network"}},
	{"bank", []string{"bank", "banking", "iban", "chase", "hsbc", "barclays", "revolut", "wise", "monzo", "n26"}},
	{"credit-card", []string{"card", "visa", "mastercard", "amex", "credit", "debit", "cvv"}},
	{"currency-btc", []string{"bitcoin", "btc"}},
	{"wallet", []string{"wallet", "crypto", "eth", "ethereum", "metamask", "ledger", "trezor", "coinbase", "binance"}},
	{"envelope", []string{"mail", "email", "imap", "smtp", "mailbox", "proton", "protonmail", "fastmail", "tutanota"}},
	{"chat-circle", []string{"chat", "matrix", "irc", "signal", "element", "zulip"}},
	{"package", []string{"npm", "pypi", "docker", "registry", "crates", "rubygems", "maven", "nuget", "packagist"}},
	{"key", []string{"token", "api", "apikey", "key", "secret", "pat"}},
	{"certificate", []string{"cert", "certificate", "gpg", "pgp", "tls", "ssl"}},
	{"device-mobile", []string{"phone", "mobile", "sim", "puk"}},
	{"globe", []string{"domain", "dns", "registrar", "namecheap", "godaddy", "porkbun", "website", "site"}},
	{"shopping-cart", []string{"shop", "shopping", "ebay", "etsy", "shopify", "aliexpress", "taobao"}},
	{"game-controller", []string{"game", "games", "xbox", "playstation", "psn", "nintendo", "epic", "gog", "battlenet"}},
	{"film-strip", []string{"netflix", "hulu", "disney", "movie", "movies", "film", "prime"}},
	{"music-notes", []string{"music", "soundcloud", "bandcamp", "tidal", "deezer"}},
	{"airplane", []string{"flight", "flights", "airline", "travel", "booking", "airbnb"}},
	{"heartbeat", []string{"health", "medical", "doctor", "insurance", "hospital"}},
	{"graduation-cap", []string{"school", "university", "uni", "edu", "college", "student"}},
	{"git-branch", []string{"git", "gitea", "forgejo", "codeberg", "bitbucket", "sourcehut"}},
	{"house", []string{"home", "house"}},
	{"user", []string{"login", "account", "user", "admin", "root"}},
}

// Icon is the Phosphor icon for an entry, found by the words of its own
// name, then of its folder (both for a service and for what it is), then
// of its category (for a service only: a category holds many kinds); ""
// when none says.
func Icon(name string) string {
	parts := strings.Split(name, "/")
	leaf := parts[len(parts)-1]
	var folder, category string
	if len(parts) > 2 {
		folder = parts[len(parts)-2]
	}
	if len(parts) > 1 {
		category = parts[0]
	}
	for _, p := range []string{leaf, folder} {
		if icon := Brand(p); icon != "" {
			return icon
		}
		if icon := kindOf(p); icon != "" {
			return icon
		}
	}
	return Brand(category)
}

// words splits a part of a name into its words, in lower case:
// "Work-GitHub.com" is work, github, com.
func words(part string) []string {
	return strings.FieldsFunc(strings.ToLower(part), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// Brand is the Phosphor logo of the service a word of part names:
// "github" of "work-github.com"; "" when none does.
func Brand(part string) string {
	for _, w := range words(part) {
		if icon, ok := brands[w]; ok {
			return icon
		}
	}
	if p := strings.ToLower(part); p == "x" || p == "x.com" {
		return "x-logo"
	}
	return ""
}

func kindOf(part string) string {
	ws := words(part)
	for _, k := range kinds {
		for _, w := range ws {
			if slices.Contains(k.words, w) {
				return k.icon
			}
		}
	}
	return ""
}

// Icons are every icon Icon may give, for the app to ship.
func Icons() []string {
	seen := map[string]bool{"x-logo": true}
	out := []string{"x-logo"}
	for _, icon := range brands {
		if !seen[icon] {
			seen[icon] = true
			out = append(out, icon)
		}
	}
	for _, k := range kinds {
		if !seen[k.icon] {
			seen[k.icon] = true
			out = append(out, k.icon)
		}
	}
	return out
}
