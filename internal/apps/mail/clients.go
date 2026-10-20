package mail

// inua's own OAuth clients, registered with Google and Microsoft for
// inua-mail (docs/mail.md, "OAuth client IDs"). An installed app's client
// is public by nature: it ships in every copy, and PKCE, not a secret,
// protects the sign-in. mail.toml's [oauth.google] and [oauth.microsoft]
// (Mail's welcome, "Your own OAuth client") take a user's own instead.
var (
	GoogleClientID    = "368064196045-6lt4tqtim9ooukql5j32ks2qtj2nlq89.apps.googleusercontent.com"
	MicrosoftClientID = "8cc1f8f5-1845-401a-990c-b5578d70461e"

	// GoogleClientSecret is the Google desktop client's secret, which
	// Google's token page asks for. A release sets it at build time from
	// CI (INUA_GOOGLE_CLIENT_SECRET; make reads the same variable), as
	// GitHub reports a GOCSPX secret in a public repository to Google.
	GoogleClientSecret = ""
)
