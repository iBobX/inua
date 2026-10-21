# go-imap, as inua carries it

This is github.com/emersion/go-imap/v2 at v2.0.0-beta.8 (MIT, see
LICENSE), with one patch. inua's go.mod points at it with a `replace`.
The command-line tools (cmd/) and the tests are left out. To take a new
go-imap, copy the module over this again and put the patch back, or drop
this copy once go-imap reads such a part itself.

## Patched: imapclient/fetch.go

- A message part (message/rfc822 or message/global) with no
  parenthesis where its envelope should be is read as an opaque part.
  Such a part goes on, after its size, with its envelope, its structure
  and its lines; Gmail gives an encoded one (the returned message of a
  bounce, message/global in base64) with only the extension's fields
  there: `("MESSAGE" "GLOBAL" NIL NIL NIL "BASE64" 8936 NIL NIL NIL)`.
  go-imap failed on it with `in body-type-1part: imapwire: expected '(',
  got "N"`, and as Mail fetches the structure of many messages at once,
  the whole mailbox stopped syncing. Those fields are now read as any
  part's extension; the part's MessageRFC822 is nil. `readEnvelope` is
  split so the envelope can be read after its parenthesis
  (`readEnvelopeFields`).
- inua's regression tests: internal/apps/mail/bodystructure_test.go (the
  bounce's shape, and a message part as servers usually give it).
