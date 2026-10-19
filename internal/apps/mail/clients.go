package mail

// inua's own OAuth clients, registered with Google and Microsoft for
// inua-mail (docs/mail.md, "OAuth client IDs"). An installed app's client
// is public by nature: it ships in every copy, and PKCE, not a secret,
// protects the sign-in. mail.toml's [oauth.google] and [oauth.microsoft]
// take a user's own instead; a build can set them too
// (-ldflags "-X github.com/lyricat/inua/internal/apps/mail.GoogleClientID=…").
var (
	GoogleClientID     = ""
	GoogleClientSecret = "" // a desktop client's; Google does not treat it as secret
	MicrosoftClientID  = ""
)
