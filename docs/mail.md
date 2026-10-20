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
│ ▯                │ ⌕ Search, or ask…   [✎]  │ ↩ ↩↩ ↪          ▣ 🗑 ⊘ ▭ ☆ ✉ ⋯ │
│                  │ ALL UNREAD FILES REPLY   │───────────────────────────────│
│ All inboxes   3  │ ■ Ana Souza       18:06  │ The Q3 report                 │
│ Needs reply   1  │   The Q3 report       3  │ (✦ Summarize) (✦ Reply ideas) │
│ Starred          │   Thanks! Can you send…  │ (A) Ana Souza  ana@studio.dev │
│ ┌ A Ana Souza    │   REPLY  FILE            │     to me, Ben    Tue 18:06   │
│ Inbox         3  │ □ Hetzner Online   Sun   │ Thanks! Can you send the …    │
│ Drafts · Sent    │   Your invoice R0021455  │ [PDF q3-draft.pdf · Save…]    │
│ 1 more folder    │   FILE                   │ ┌ Reply to Ana…             ┐ │
│ ■ Up to date [⚙] │                          │ └───────────────────────────┘ │
└──────────────────┴──────────────────────────┴───────────────────────────────┘
```

- **The sidebar's toggle** at the top left hides the index (`F9`), as
  Apple Mail's does; while it is hidden, the toggle sits beside the
  search. ✎ beside the search writes a message (`c`).
- **The foot** says how the sync is, in words: up to date, when it last
  checked, checking, or which accounts cannot connect; ⚙ opens the
  settings.
- **The index**: *All inboxes*; *Needs reply* (conversations tagged
  REPLY whose newest message is not yours: only the assistant's sorting
  tags REPLY, so it is empty with sorting off); *Starred*; *Unsent* when
  drafts are kept; then each account in a panel of its own: its photo
  (a Google account's) or initial, its name and address, then its
  mailboxes in the same order for every account (Inbox, Drafts, Sent,
  Archive, Spam, Trash), the others behind *N more folders*. A click on
  the account's head opens its settings. An account that cannot connect
  says so under its name, with *Sign in* when its sign-in ended.
- **The list**: a conversation once, by its newest message: the face
  of who wrote, and beside it, each line starting at one edge, who wrote
  and when, the subject and how many messages, the text in one to three
  lines (Settings → Reading → *Preview*, two at first), and a mark for
  what to act on: REPLY (someone asks you something), WAITING (you wrote
  last), SUSPICIOUS (the phishing checks), FILE. The square by the time
  is unread. A face is the photo of one of your own accounts, else the
  logo of the service the address is at (GitHub, Google, Spotify… the
  logos Pass draws; not for a mailbox anyone can have, as gmail.com),
  else the initial; on a round mark in one of six tones of the theme,
  the same for an address each time.
  ALL, UNREAD, FILES and REPLY filter it; *MARK READ*, at the row's end
  while the list has unread mail, marks every conversation of the view
  read, on the server too. A mailbox fetched the first time shows the
  loader until it comes. Whether the sidebar is hidden and the
  assistant's panel open is kept for the next start.
- **The conversation**: a bar of its actions stays over it: reply, reply
  all and forward; archive, trash, spam, move, star and unread; ⋯ for
  showing the newest message as text or formatted, opening it in the
  browser, and the keys. Under the subject, the assistant's offers as
  chips (*Summarize*, *Reply ideas*). It reads in a column no wider
  than a line is comfortable to follow (720 pixels, grown with the
  text), in the middle of a wider pane; its text is at the size
  Settings → Reading → *Text size* says, 85% to 150%, formatted mail
  zoomed to it. Each message is headed by its
  sender's face, name and address, who it went to and when; older
  ones fold to a line (a click opens one); quoted text and signatures
  wait behind •••; attachments are cards: a click opens one (images and
  PDFs in Viewer, the rest with the desktop's app), *Save…* keeps it.

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
replies to all, `f` forwards, `e` archives, `d` or Delete moves it to
the Trash (in the Trash: deletes it, after asking), `v` moves it, `s`
stars, `u` marks it unread, `!` moves it to Spam, `z` undoes the last
move, `F9` hides or shows the sidebar, `Ctrl+,` opens the settings. In the composer and the quick reply,
`Ctrl+Enter` sends; in the composer, `Ctrl+J` opens AI's panel, goes
into it, or closes it from inside it, and `Ctrl+W` closes the window,
the draft kept.

The window's title counts the unread.

## Accounts

With no account, and on *Add account* in the settings, Mail shows the welcome in its
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

[reading]
text = false                    # show HTML mail as text
images = "ask"                  # pictures from the web: ask, always or never
images_from = []                # senders whose pictures always load
keep_unread = false             # opening a conversation does not mark it read
preview_lines = 2               # lines of each conversation's text in the list, 1 to 3
text_size = 100                 # a message's text, in percent: 85, 100, 115, 130 or 150

[writing]
undo_send = 5                   # seconds to take a send back; -1 sends at once

[assistant]
off = false                     # true turns every AI feature of Mail off
sort = false                    # sort new mail with the assistant
summary_lang = ""               # the summaries' language: en, zh-Hans, zh-Hant, ja, ko, es, fr, de, it, pt or ru; empty: the conversation's

[window]
hide_sidebar = false            # kept as the window was left
assistant = false               # the right panel open
panel_hid_sidebar = false       # the panel hid the sidebar, to make room
compose_assistant = false       # the composer's AI panel open
```

**Settings** (Ctrl+, ⚙ at the index's foot, or a click on an account's
head) is a window of its own, in the look of inua's Settings: each account (its name, signature, signing in again, removing
it; *Add account*), Reading, Writing, the assistant's sorting, and Advanced (your own
OAuth client, the cache kept and *Clear*).

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
ways to sign in in the welcome, or Settings → Advanced; empty fields use
inua's.
The client ID goes to `mail.toml`, the secret to your keyring (as the
accounts' tokens), and `mail.toml` notes `secret_in_keyring = true`.
A `client_secret` written in `mail.toml` by hand still works. An account signed in with one client signs in
again after the client changes.

```toml
[oauth.google]
client_id = "123-abc.apps.googleusercontent.com"
secret_in_keyring = true        # the welcome keeps the secret in the keyring

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
newest 2000 messages of a mailbox, and older ones as the list is
scrolled to its end; after
that, what came, what changed and what went. A second connection waits
in IDLE on each inbox, so new mail shows at once, with a notification
when the window is not in front. Reading, starring, archiving and
deleting change the list at once and the server after; when the server
refuses, the change goes back and the conversation says why.

The cache can be deleted at any time; drafts are not in it
(`~/.local/share/inua/mail/drafts/`).

## Writing

The composer is a window of its own, floating over Mail (its class,
`io.github.lyricat.Inua.Mail.Compose`, has a rule of its own in
`inua.conf`): From (when there are several accounts), To, Cc and Bcc,
the subject, the text and the files attached (*Attach…*, or drop files
on the window). Typing in To, Cc or Bcc
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
five seconds (Settings → Writing: off, 5, 10, 20 or 30), in which *Undo*
takes it back. A copy goes to Sent (Gmail
and Outlook keep their own).

## AI

Mail uses inua's assistant (Settings → AI), only when asked, except the
sorting, which is off until turned on. *Use AI in Mail* (Mail's Settings
→ AI) turns every bit of it off, and then nothing of your mail goes to a
model.

The right panel opens from ✦ in the conversation's bar (in a window too
narrow for both, the sidebar makes room, and comes back when it closes):
*Sum it up* and *Suggest replies*, then *Is it
safe?*: each check's finding for the newest message not yours, and AI's
judgment when asked (*Ask AI*). A reply it writes goes into the quick
reply under the conversation, marked *Drafted by the assistant*, with
*Undo*, for you to read and send. With AI off the panel is *Checks*,
opened from a shield: the checks alone, which run on this computer.
Whether it is open, and whether the sidebar is shown, are kept for the
next start.

The composer has an AI panel of its own at its right edge (✦ at the top
right, or `Ctrl+J`). While nothing is written it offers a line to say
what to write and the conversation's reply ideas; once something is,
your own words for how to change it, *Shorter*, *Friendlier*, *More
formal*, *Fix spelling* and *Translate* into one of eleven languages;
with text selected, only the selection changes. What AI writes comes
into the text tinted, a suggestion: *Keep* (Enter) makes it yours, *Try
again* asks for another version (each one kept, ‹ › between them),
*Discard* (Esc, or closing the panel) puts your text back, and *Yours*
shows it for a moment to compare. While a suggestion waits, Send waits
too, and the draft kept is your text.

| Where | What | What it sends |
| --- | --- | --- |
| The panel: *Sum it up* | Two or three sentences, then what you are asked to do, with dates, in the conversation's language, or the one chosen in Settings → AI (*Summary language*); kept until the conversation gets a new message | The messages' text, quotes cut |
| The panel: *Suggest replies* | Three short replies; the one picked is written out in the quick reply | The last six messages; to write it out, also three of your sent messages kept on this computer |
| An attachment: *Sum up* (PDF, Word, OpenDocument, HTML, text) | What it is, the figures, dates and names that matter, what it asks of you | Its text, read on this computer (a PDF through poppler, as Viewer does), and the message it came with |
| The composer's panel: a reply idea, or a line of what to write | The text, in the conversation's language and your voice, as a suggestion | The line, the conversation, three of your sent messages kept on this computer |
| The composer's panel: a change | Shorter, friendlier, more formal, spelling and grammar, a translation, or as your own words say, as a suggestion | The text you wrote (not the signature or the quote), or only the selection |
| The search: a sentence and Enter | The search's operators, shown in the entry, then searched | The sentence and today's date |
| The panel's *Is it safe?*, or a warning's *Ask AI* | Its verdict, two or three reasons, and what to do, under the checks' own verdict and findings | The checks' findings, the sender, the subject, the links and the text |
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

- HTML is shown as sent, by WebKitGTK 6.0 when the system has it
  (`libwebkitgtk-6.0`, loaded on the first HTML message, so Mail starts
  as fast without it): its layout, tables and styles, on white, sized to
  its page and zoomed out to fit a narrow column. The mail's scripts do
  not run; frames, forms, refreshes and event attributes are taken out
  before it loads; a content policy in the page lets nothing load from
  the network unless its pictures are allowed; links open in your
  browser. Without WebKitGTK, Mail draws it itself: headings, emphasis,
  links, lists, quotes, code and pictures, in its own type. The part of
  a reply quoting earlier mail folds behind •••. *Show as text* in ⋯ (or
  Settings → Reading) shows the words alone; *Open in browser* shows it
  in the browser.
- Pictures a message carries show at once. Pictures from the web are
  asked for: a line says how many are hidden, with *Show* and *Always
  from this sender*. Settings → Reading can load them always or
  never; a message the phishing checks find suspicious never loads them
  by itself.
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
| `internal/apps/mail` | No GTK: accounts and `mail.toml`, secrets, OAuth and XOAUTH2, the IMAP session (sync, actions, IDLE, search), SMTP, parsing and building messages, HTML to text and to formatted blocks, pictures, conversations, search, sorting, the phishing checks, the prompts |
| `internal/mailui`, `ui/mail.blp`, `ui/mail-compose.blp` | The window, the welcome, the settings, the composer, the formatted reader |
| `cmd/inua-mail` | The program |

It uses emersion's go-imap v2, go-message, go-smtp and go-sasl (MIT).
The tests run against a real IMAP server in memory, go-smtp's server and
a fake OAuth provider.
