# Mail

`inua-mail` reads and writes mail from all your accounts in three columns:
the mailboxes on the left, the conversations of the one chosen in the
middle, the conversation on the right. Gmail and Outlook sign in on their
own page in your browser; any other account signs in to its IMAP and SMTP
servers with a password. The assistant helps where it saves time, and
only when asked (section *The assistant*).

```
inua-mail [mailto:LINK]       open the window; a composer for a mailto: link
inua-mail --accounts          the accounts in mail.toml
inua-mail --sync ACCOUNT      sign in and sync an account's inbox, with timings
inua-mail --check FILE.eml    the phishing checks on a message
```

## The window

```
┌──────────────────┬──────────────────────────┬───────────────────────────────┐
│ ■ IDLE · 20:41:07 [✎]│ ⌕ Search, or ask…    │ The Q3 report                 │
│ 1,204 MSG · 3 UNREAD │ ALL UNREAD FILES REPLY │ SUMMARY · ASSISTANT         │
│ All inboxes   3  │                          │                               │
│ Needs reply   1  │ ■ Ana Souza       18:06  │ Ana needs the numbers by Fri… │
│ Starred          │   The Q3 report       3  │ Summarize · Reply ideas       │
│ STUDIO  ana@st…  │   Thanks! Can you send…  │ ───────────────────────────── │
│ Inbox         3  │   REPLY  FILE            │ Ana Souza  Hi both, Here's…   │
│ Drafts · Sent    │ □ Hetzner Online   Sun   │ Lyric      Looks right to…    │
│ Archive · Spam   │   Your invoice R0021455  │ Ana Souza  to me, Ben  18:06  │
│ Trash            │   FILE                   │ Thanks! Can you send the …    │
│ 1 more folder    │                          │ [PDF q3-draft.pdf · Save…]    │
│ Add account      │                          │ [Reply]  Reply all  Forward   │
└──────────────────┴──────────────────────────┴───────────────────────────────┘
```

- **The readout** at the top: whether Mail is waiting for new mail
  (IDLE), syncing or offline, when it last synced, and the messages kept
  from the inboxes with how many are unread. ✎ writes a message.
- **The index**: *All inboxes*; *Needs reply* (conversations tagged
  REPLY whose newest message is not yours); *Starred*; *Unsent* when
  drafts are kept; then each account under its name, its mailboxes in
  the same order for every account (Inbox, Drafts, Sent, Archive, Spam,
  Trash), the others behind *N more folders*, which also shows *Remove
  account…*. An account that cannot connect says so under its name, with
  *Sign in* when its sign-in ended.
- **The list**: a conversation once, by its newest message: who wrote,
  when, the subject and how many messages, the first line, and a mark for
  what to act on: REPLY (someone asks you something), WAITING (you wrote
  last), SUSPICIOUS (the phishing checks), FILE. The square is unread.
  ALL, UNREAD, FILES and REPLY filter it.
- **The conversation**: older messages folded to a line (a click opens
  one), the newest open; quoted text and signatures behind *Show quoted
  text*; attachments as cards: a click opens one (images and PDFs in
  Viewer, the rest with the desktop's app), *Save…* keeps it.

**Invitations** show as a card above the message's text: the event, when
in your time, where (with *Join* for a video meeting), who invited whom
and how many accepted. *Accept*, *Maybe* and *Decline* send your answer
to the organizer (an iTIP reply their calendar takes in), and the card
keeps it; *Add to calendar* opens the event with your calendar app. A
cancelled event says so. Google's, Outlook's (Windows zone names) and
other calendars' invitations are read.

**Acting on a conversation.** A pointer over a row shows its actions in
place of the time: archive, delete, read or unread, star. Archiving,
deleting and moving take the conversation out of the list at once, with
an *Undo* bar for a few seconds (`z`); the server is told when the bar
goes, so an undo never reaches it. The next conversation opens in its
place. `v` moves to any folder: a short list, narrowed as you type,
Enter takes the first.

**Quick reply.** Under a conversation, a field answers its newest
message without a window: it quotes what it answers, under your
signature, and sends with `Ctrl+Enter` after the same five seconds to
take it back. *Open in a window* moves what you wrote to the composer.

Keys (`?` lists them): `j`/`k` or the arrows choose a conversation, `/`
or `Ctrl+F` searches, `c` composes; on a conversation `r` replies, `a`
replies to all, `f` forwards, `e` archives, `#` or Delete moves it to
the Trash (in the Trash: deletes it, after asking), `v` moves it, `s`
stars, `u` marks it unread, `!` moves it to Spam, `z` undoes the last
move. In the composer and the quick reply, `Ctrl+Enter` sends.

The window's title counts the unread.

## Accounts

With no account, and on *Add account*, Mail shows the welcome in its
window: three steps, ADDRESS, SIGN IN and FIRST SYNC. Type your address;
the way to sign in is worked out from it (Gmail → Google, Outlook,
Hotmail and Live → Microsoft, the rest → IMAP), or click another way (a
Google Workspace or Microsoft 365 address on your own domain). The
sign-in and the first sync show as a log; *Open the inbox* ends it. Esc
goes back a step.

**Google and Microsoft** open the provider's sign-in page in your
browser (the link is on the page too, with Copy). Sign in there and
allow Mail to read and send your mail; Mail learns your address from the
answer and never sees your password. It keeps a refresh token in your
keyring and signs in to IMAP and SMTP with `XOAUTH2`.

**IMAP** takes your password and name; the two servers are filled in
for Fastmail, iCloud, Yahoo, Proton Mail Bridge, Zoho, GMX, AOL, QQ and
163, and guessed (`imap.DOMAIN:993`, `smtp.DOMAIN:465`) for the rest,
shown to check. Port 993 and 465 use TLS; any other port starts TLS
after connecting, and a server without TLS is refused. Mail signs in to
both servers before it keeps the account. For Gmail, iCloud or Yahoo with
two-step sign-in, use an app password.

Accounts are kept in `~/.config/inua/mail.toml`; passwords and tokens in
the Secret Service (inua-keyring, gnome-keyring …), else in
`~/.local/share/inua/mail-secrets.json`, which only you can read.

```toml
[[account]]
id = "ana-studio"
name = "Ana Souza"
address = "ana@studio.dev"
provider = "imap"               # google, microsoft or imap
signature = "Ana"
imap = "imap.fastmail.com:993"
smtp = "smtp.fastmail.com:465"
username = ""                   # the address when empty

[assistant]
sort = false                    # sort new mail with the assistant
```

### OAuth client IDs

Google and Microsoft give an app a client ID when its developer
registers it. inua's own IDs are in `internal/apps/mail/clients.go`: an
installed app's client is public (it ships in every copy; PKCE protects
the sign-in). Google's desktop client also has a secret, which its token
page asks for; it is not in the source, as GitHub reports a `GOCSPX-`
secret found in a public repository to Google. A release takes it from
the repository secret `INUA_GOOGLE_CLIENT_SECRET` (the release workflow
passes it to GoReleaser), and `make build` from the same environment
variable:

```
INUA_GOOGLE_CLIENT_SECRET=GOCSPX-… make build
```

A build without it marks Google *no client ID*.

To use a client of your own, open *Your own OAuth client…* under the
ways to sign in in the welcome (Add account), or put it in `mail.toml`;
empty fields use inua's. An account signed in with one client signs in
again after the client changes.

```toml
[oauth.google]
client_id = "123-abc.apps.googleusercontent.com"
client_secret = "GOCSPX-…"      # a desktop client's; Google does not treat it as secret

[oauth.microsoft]
client_id = "00000000-0000-0000-0000-000000000000"
tenant = "common"               # or your organization's
```

- **Google**: in the Google Cloud console, create a project, enable the
  Gmail API, set up the OAuth consent screen (add yourself as a test
  user; Gmail's scope is restricted, so an unverified app serves up to
  100 test users), then create an OAuth client of the type *Desktop
  app*. Scopes: `https://mail.google.com/`, `openid`, `email`.
- **Microsoft**: in the Azure portal, *App registrations* → *New
  registration*, accounts in any organizational directory and personal
  Microsoft accounts; under *Authentication* add the platform *Mobile and
  desktop applications* with the redirect URI `http://localhost`, and
  allow public client flows. API permissions (delegated):
  `IMAP.AccessAsUser.All` and `SMTP.Send` of Office 365 Exchange Online,
  `offline_access`, `openid`, `email`.

Without a client ID the welcome marks Google and Microsoft *no client
ID*; the account can still be added as IMAP with an app password where
the provider allows one.

## Keeping in step

Mail opens with what it kept from last time (the indexes in
`~/.cache/inua/mail/`), then connects. The first time, it fetches the
newest 2000 messages of a mailbox (*Load older* fetches more); after
that, what came, what changed and what went. A second connection waits
in IDLE on each inbox, so new mail shows at once, with a notification
when the window is not in front. Reading, starring, archiving and
deleting change the list at once and the server after; when the server
refuses, the change goes back and the conversation says why.

The cache can be deleted at any time; drafts are not in it
(`~/.local/share/inua/mail/drafts/`).

## Writing

The composer is a window of its own: From (when there are several
accounts), To, Cc and Bcc, the subject, the text and the files attached
(*Attach…*, or drop files on the window). Typing in To, Cc or Bcc
suggests the people you mail, under the field: those you write to
first, then those who write to you, lists and automated senders left
out; Tab or Enter takes the first, ↓ goes into the list. A reply quotes what it answers
under your signature and keeps the conversation's headers; Reply all
leaves your own addresses out; Forward attaches the original's files.

The draft is kept on this computer a few seconds after you stop typing,
and a copy in the account's Drafts folder once a minute while it changes
and when the window closes, each copy in place of the one before; so a
draft follows you to your other devices. In Drafts, *Continue writing*
opens it again (its people, text, files and the conversation it
answers) and *Discard* moves it to the Trash; sending it removes its
copy. A draft the server could not take (offline, or an account with no
Drafts folder) stays on this computer, under *Unsent*. Send waits
five seconds, in which *Undo* takes it back. A copy goes to Sent (Gmail
and Outlook keep their own).

## The assistant

Mail uses inua's assistant (Settings → AI), only when asked, except the
sorting, which is off until turned on:

| Where | What | What it sends |
| --- | --- | --- |
| A conversation: *Summarize* (three messages or more, or a long one) | Two or three sentences, then what you are asked to do, with dates; kept until the conversation gets a new message | The messages' text, quotes cut |
| A conversation: *Reply ideas* | Three short replies; one opens the composer and drafts it | The last six messages |
| An attachment: *Sum up* (PDF, Word, OpenDocument, HTML, text) | What it is, the figures, dates and names that matter, what it asks of you | Its text, read on this computer (a PDF through poppler, as Viewer does), and the message it came with |
| The composer: *Draft* | The text from a line of what to say, in the conversation's language and your voice | The line, the conversation, three of your sent messages kept on this computer |
| The composer: *Polish* | Shorter, friendlier, more formal, spelling and grammar, or a translation; *Undo* puts your text back | The text you wrote (not the signature or the quote) |
| The search: a sentence and Enter | The search's operators, shown in the entry, then searched | The sentence and today's date |
| A suspicious message: *Ask the assistant* | Its judgment after the checks | The checks' findings, the sender, the subject, the links and the text |
| New mail (`[assistant] sort = true`) | A tag for what the rules do not settle | The sender, the subject, the first 400 characters |

What the assistant writes in the composer is said to be its own; read it
before sending. It never sends, moves or deletes a message.

Sorting without the assistant: a message to you with a question is
REPLY; your own last message in a conversation, WAITING; automated
receipts, RECEIPT; lists, NEWS; other automated mail, NOTICE. The list
shows REPLY and WAITING; the rest stay as data for the filters.

## Search

Words are looked for in the subject, the people and the first line, in
the cache at once; Enter searches the server too. Operators:
`from:` `to:` `subject:` `has:attachment` `is:unread` `is:read`
`is:starred` `after:2026-09-01` `before:2026-10-01` `newer_than:2w`
`older_than:1y` and `"exact words"`. A sentence of four words or more
("the invoice Hetzner sent last month") asks the assistant on Enter.

## Reading safely

- HTML is shown as text: paragraphs, lists, quotes, and each link as its
  text with where it goes after it. No styles, scripts or remote images,
  so no tracking pixel loads. *Open in browser* shows the message as
  sent, and may load its remote images.
- No read receipts are sent.
- Attachments open from `$XDG_RUNTIME_DIR/inua-mail/`, which only you can
  read and which is emptied when Mail quits.
- Every message not yours goes through the phishing checks; one that is
  not fine says so at the top, in one line, with its checks behind it:

1. **Did the sender's server vouch for it?** SPF, DKIM and DMARC from
   your provider's `Authentication-Results`. DMARC failing is suspicious.
2. **Is it who it says it is?** A name showing another address; a
   lookalike domain (`paypa1.com`), Punycode or mixed alphabets; a brand
   named from a domain that is not the brand's.
3. **Do replies go to the sender?** `Reply-To` on another domain.
4. **Do its links go where they say?** Link text naming another domain
   than the link's; links to bare IP addresses.
5. **Have you written to them?** A good sign, not proof.

`inua-mail --check FILE.eml` prints the same checks; it exits 3 for a
suspicious message.

## Code

| Where | What |
| --- | --- |
| `internal/apps/mail` | No GTK: accounts and `mail.toml`, secrets, OAuth and XOAUTH2, the IMAP session (sync, actions, IDLE, search), SMTP, parsing and building messages, HTML to text, conversations, search, sorting, the phishing checks, the prompts |
| `internal/mailui`, `ui/mail.blp`, `ui/mail-compose.blp` | The window, the welcome, the composer |
| `cmd/inua-mail` | The program |

It uses emersion's go-imap v2, go-message, go-smtp and go-sasl (MIT).
The tests run against a real IMAP server in memory, go-smtp's server and
a fake OAuth provider.
