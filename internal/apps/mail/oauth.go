package mail

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

// Endpoint is a provider's OAuth pages and what inua asks it for.
type Endpoint struct {
	AuthURL, TokenURL string
	UserInfo          string // where the account's profile is read with its token; "" for none
	Book              string // the account's address book over CardDAV, %s its address; "" for none
	Scopes            []string
	Extra             url.Values // more for the authorization page
	Client            OAuthClient
	// secret reads the client's secret from the keyring when it is
	// needed, at a token's trade, off the main thread: the keyring may ask
	// for a passphrase.
	secret func() (string, error)
}

// SecretKey names a provider's client secret among the keyring's items.
const SecretKey = "@oauth"

// ClientSecretKind is the kind of a provider's client secret in the
// keyring (account SecretKey).
func ClientSecretKind(p Provider) string { return string(p) + "-client-secret" }

// ErrNoClient is signing in with a provider that has no client ID.
var ErrNoClient = errors.New("no client ID")

// Endpoint is provider p's, with the user's client or inua's.
func (s Settings) Endpoint(p Provider) (Endpoint, error) {
	switch p {
	case Google:
		c := s.OAuth.Google
		if c.ClientID == "" {
			c = OAuthClient{ClientID: GoogleClientID, ClientSecret: GoogleClientSecret}
		}
		// Google's token page refuses a desktop client without its secret.
		if c.ClientID == "" || (c.ClientSecret == "" && !c.SecretInKeyring) {
			return Endpoint{}, fmt.Errorf("%w: %s", ErrNoClient, i18n.T("Google sign-in needs a client ID and its secret: this build has none. Add your own under Your own OAuth client (docs/mail.md), or add the account as IMAP with an app password."))
		}
		return Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
			UserInfo: "https://openidconnect.googleapis.com/v1/userinfo",
			Book:     GoogleBook,
			Scopes:   []string{"https://mail.google.com/", "https://www.googleapis.com/auth/carddav", "openid", "email", "profile"},
			Extra:    url.Values{"access_type": {"offline"}, "prompt": {"consent"}},
			Client:   c,
			secret:   s.keyringSecret(Google, c),
		}, nil
	case Microsoft:
		c := s.OAuth.Microsoft
		if c.ClientID == "" {
			c = OAuthClient{ClientID: MicrosoftClientID, Tenant: c.Tenant}
		}
		if c.ClientID == "" {
			return Endpoint{}, fmt.Errorf("%w: %s", ErrNoClient, i18n.T("Microsoft sign-in needs a client ID: this build has none. Add your own under Your own OAuth client (docs/mail.md)."))
		}
		tenant := c.Tenant
		if tenant == "" {
			tenant = "common"
		}
		base := "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0/"
		return Endpoint{
			AuthURL:  base + "authorize",
			TokenURL: base + "token",
			Scopes: []string{"https://outlook.office.com/IMAP.AccessAsUser.All", "https://outlook.office.com/SMTP.Send",
				"offline_access", "openid", "email", "profile"},
			Extra:  url.Values{"prompt": {"select_account"}},
			Client: c,
			secret: s.keyringSecret(Microsoft, c),
		}, nil
	}
	return Endpoint{}, fmt.Errorf("%s does not sign in with OAuth", p)
}

// keyringSecret reads a user's client's secret kept in the keyring, when
// mail.toml says it is there and holds none itself.
func (s Settings) keyringSecret(p Provider, c OAuthClient) func() (string, error) {
	if !c.SecretInKeyring || c.ClientSecret != "" || s.Keyring == nil {
		return nil
	}
	k := s.Keyring
	return func() (string, error) {
		v, err := k.Get(SecretKey, ClientSecretKind(p))
		if err == nil && v == "" {
			err = errors.New(i18n.T("the client's secret is not in the keyring: type it again under Your own OAuth client"))
		}
		return v, err
	}
}

// Token is what a provider gives for a sign-in.
type Token struct {
	Access  string
	Refresh string
	Expiry  time.Time
	Email   string // from the ID token, when there is one
	Name    string // the user's name, from the ID token
	Picture string // the user's photo's address, from the ID token
}

// SignIn is a sign-in waiting for the browser: open URL, then Wait.
type SignIn struct {
	URL      string
	ep       Endpoint
	verifier string
	redirect string
	ln       net.Listener
	srv      *http.Server
	result   chan signInResult
	once     sync.Once
}

type signInResult struct {
	code string
	err  error
}

// StartSignIn listens on a loopback port for the provider's redirect and
// makes the authorization page's address, with a PKCE challenge and a
// random state (RFC 8252). login_hint, when not empty, fills the page's
// address field.
func StartSignIn(ep Endpoint, loginHint string) (*SignIn, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &SignIn{ep: ep, ln: ln, verifier: rand.Text() + rand.Text(), result: make(chan signInResult, 1)}
	s.redirect = fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)
	state := rand.Text()
	sum := sha256.Sum256([]byte(s.verifier))
	q := url.Values{
		"client_id":             {ep.Client.ClientID},
		"redirect_uri":          {s.redirect},
		"response_type":         {"code"},
		"scope":                 {strings.Join(ep.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	if loginHint != "" {
		q.Set("login_hint", loginHint)
	}
	for k, v := range ep.Extra {
		q[k] = v
	}
	s.URL = ep.AuthURL + "?" + q.Encode()
	s.srv = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		v := r.URL.Query()
		var res signInResult
		switch {
		case v.Get("state") != state:
			res.err = errors.New(i18n.T("the sign-in page answered for another request"))
		case v.Get("error") != "":
			res.err = errors.New(strings.TrimSpace(v.Get("error") + ": " + v.Get("error_description")))
		case v.Get("code") == "":
			res.err = errors.New(i18n.T("the sign-in page gave no code"))
		default:
			res.code = v.Get("code")
		}
		page(w, res.err)
		select {
		case s.result <- res:
		default:
		}
	})}
	go s.srv.Serve(ln)
	return s, nil
}

// page is what the browser shows after the redirect.
func page(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title, text := i18n.T("Signed in"), i18n.T("You can close this tab and go back to Mail.")
	if err != nil {
		title, text = i18n.T("Not signed in"), err.Error()
	}
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%[1]s</title>
<style>body{font:15px system-ui,sans-serif;background:#16171a;color:#e8e6e1;display:grid;place-items:center;height:90vh;margin:0}
div{max-width:28em}h1{font-weight:500;font-size:20px}p{color:#a9a69f}</style>
<div><h1>%[1]s</h1><p>%[2]s</p></div>`, html.EscapeString(title), html.EscapeString(text))
}

// Wait waits for the browser's redirect and trades its code for tokens.
func (s *SignIn) Wait(ctx context.Context) (Token, error) {
	defer s.Close()
	var res signInResult
	select {
	case <-ctx.Done():
		return Token{}, ctx.Err()
	case res = <-s.result:
	}
	if res.err != nil {
		return Token{}, res.err
	}
	return s.ep.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {res.code},
		"redirect_uri":  {s.redirect},
		"code_verifier": {s.verifier},
	})
}

// Redirect is the address the provider sends the browser back to.
func (s *SignIn) Redirect() string { return s.redirect }

// Close stops listening.
func (s *SignIn) Close() {
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.srv.Shutdown(ctx)
	})
}

// ErrSignInAgain is a refresh token the provider no longer takes: the
// user signs in again.
var ErrSignInAgain = errors.New("sign in again")

// Refresh trades a refresh token for a new access token.
func (ep Endpoint) Refresh(ctx context.Context, refresh string) (Token, error) {
	t, err := ep.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
	if t.Refresh == "" {
		t.Refresh = refresh // the same one goes on
	}
	return t, err
}

func (ep Endpoint) token(ctx context.Context, form url.Values) (Token, error) {
	form.Set("client_id", ep.Client.ClientID)
	secret := ep.Client.ClientSecret
	if secret == "" && ep.secret != nil {
		var err error
		if secret, err = ep.secret(); err != nil {
			return Token{}, err
		}
	}
	if secret != "" {
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	var body struct {
		Access      string `json:"access_token"`
		Refresh     string `json:"refresh_token"`
		ExpiresIn   int64  `json:"expires_in"`
		IDToken     string `json:"id_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Token{}, fmt.Errorf("the token page: %s: %w", resp.Status, err)
	}
	if body.Error != "" || resp.StatusCode != http.StatusOK || body.Access == "" {
		err := fmt.Errorf("the token page: %s", strings.TrimSpace(body.Error+" "+firstLine(body.Description)))
		if body.Error == "invalid_grant" {
			err = fmt.Errorf("%w (%v)", ErrSignInAgain, err)
		}
		return Token{}, err
	}
	t := Token{Access: body.Access, Refresh: body.Refresh}
	t.Email, t.Name, t.Picture = identity(body.IDToken)
	if body.ExpiresIn > 0 {
		t.Expiry = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return t, nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(strings.TrimSuffix(s, "\r"))
}

// identity reads the address and the name from an ID token's claims.
// The token came from the provider over TLS in answer to inua's own
// request, so its signature is not checked.
func identity(idToken string) (email, name, picture string) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", "", ""
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", "", ""
	}
	var claims struct {
		Email    string `json:"email"`
		Username string `json:"preferred_username"`
		Name     string `json:"name"`
		Picture  string `json:"picture"`
	}
	if json.Unmarshal(b, &claims) != nil {
		return "", "", ""
	}
	email = claims.Email
	if email == "" && strings.Contains(claims.Username, "@") {
		email = claims.Username
	}
	if !strings.HasPrefix(claims.Picture, "https://") {
		claims.Picture = ""
	}
	return email, claims.Name, claims.Picture
}

// Tokens gives an account's access token, refreshed before it ends.
type Tokens struct {
	mu      sync.Mutex
	ep      Endpoint
	refresh string
	tok     Token
	saved   func(refresh string) error // a new refresh token is kept
}

// NewTokens starts from a refresh token; saved, when not nil, is called
// when the provider gives a new one.
func NewTokens(ep Endpoint, refresh string, saved func(string) error) *Tokens {
	return &Tokens{ep: ep, refresh: refresh, saved: saved}
}

// Access returns an access token valid for a minute at least.
func (t *Tokens) Access(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tok.Access != "" && time.Until(t.tok.Expiry) > time.Minute {
		return t.tok.Access, nil
	}
	if t.refresh == "" {
		return "", ErrSignInAgain
	}
	tok, err := t.ep.Refresh(ctx, t.refresh)
	if err != nil {
		return "", err
	}
	if tok.Refresh != t.refresh && t.saved != nil {
		if err := t.saved(tok.Refresh); err != nil {
			return "", err
		}
	}
	t.refresh, t.tok = tok.Refresh, tok
	return tok.Access, nil
}

// Expire forgets the access token, after a server refused it.
func (t *Tokens) Expire() {
	t.mu.Lock()
	t.tok = Token{}
	t.mu.Unlock()
}

// xoauth2 is the SASL mechanism Google and Microsoft take with a bearer
// token.
type xoauth2 struct{ user, token string }

func (x xoauth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}

// Next answers the server's error (a JSON challenge) with nothing, so it
// ends the exchange with its failure.
func (x xoauth2) Next([]byte) ([]byte, error) { return []byte{}, nil }
