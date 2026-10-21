package mail

import (
	"bufio"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// A bounce as Gmail describes it: a delivery report whose returned message
// is a message/global in base64 with no envelope, structure or lines after
// its size, only the extension's NILs. It reads, rather than failing the
// whole mailbox's sync (third_party/go-imap/INUA.md); the values are made
// up, the shape is Gmail's.
func TestBodyStructureOpaqueMessagePart(t *testing.T) {
	const structure = `(((("TEXT" "PLAIN" ("CHARSET" "UTF-8") NIL NIL "QUOTED-PRINTABLE" 301 7 NIL NIL NIL)` +
		`("TEXT" "HTML" ("CHARSET" "UTF-8") NIL NIL "QUOTED-PRINTABLE" 1469 30 NIL NIL NIL) "ALTERNATIVE" ("BOUNDARY" "b3") NIL NIL)` +
		`("IMAGE" "PNG" ("NAME" "icon.png") "<icon>" NIL "BASE64" 7864 NIL ("ATTACHMENT" ("FILENAME" "icon.png")) NIL) "RELATED" ("BOUNDARY" "b2") NIL NIL)` +
		`("MESSAGE" "DELIVERY-STATUS" NIL NIL NIL "7BIT" 553 NIL NIL NIL)` +
		`("MESSAGE" "GLOBAL" NIL NIL NIL "BASE64" 8936 NIL NIL NIL)` +
		` "REPORT" ("BOUNDARY" "b1" "REPORT-TYPE" "delivery-status") NIL NIL)`
	client, server := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		r := bufio.NewReader(server)
		server.Write([]byte("* OK ready\r\n"))
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		tag, _, _ := strings.Cut(line, " ")
		server.Write([]byte("* 1 FETCH (UID 7 BODYSTRUCTURE " + structure + ")\r\n" + tag + " OK done\r\n"))
		r.ReadString('\n') // the logout, unanswered
	}()
	c := imapclient.New(client, nil)
	msgs, err := c.Fetch(imap.UIDSetNum(7), &imap.FetchOptions{UID: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].BodyStructure == nil {
		t.Fatalf("messages %+v", msgs)
	}
	bs := msgs[0].BodyStructure
	mp, ok := bs.(*imap.BodyStructureMultiPart)
	if !ok || len(mp.Children) != 3 {
		t.Fatalf("structure %#v", bs)
	}
	global, ok := mp.Children[2].(*imap.BodyStructureSinglePart)
	if !ok || !strings.EqualFold(global.Subtype, "global") || global.Size != 8936 || global.MessageRFC822 != nil {
		t.Fatalf("the returned message %+v", mp.Children[2])
	}
	if !hasAttachment(bs) {
		t.Fatal("the attached image does not count")
	}
	if path, text := textPart(bs); text == nil || !strings.EqualFold(text.Subtype, "plain") || len(path) != 3 {
		t.Fatalf("text part %v %+v", path, text)
	}
}

// A message part as servers usually give it, with its envelope, structure
// and lines, reads as before.
func TestBodyStructureMessagePart(t *testing.T) {
	const structure = `(("TEXT" "PLAIN" ("CHARSET" "UTF-8") NIL NIL "7BIT" 12 1 NIL NIL NIL NIL)` +
		`("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 300 ("Mon, 13 Oct 2026 10:00:00 +0000" "Fwd" (("Ana" NIL "ana" "x.org")) NIL NIL NIL NIL NIL NIL "<a@x>")` +
		` ("TEXT" "PLAIN" NIL NIL NIL "7BIT" 20 2) 9 NIL ("ATTACHMENT" ("FILENAME" "fwd.eml")) NIL NIL)` +
		` "MIXED" ("BOUNDARY" "b1") NIL NIL NIL)`
	client, server := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		r := bufio.NewReader(server)
		server.Write([]byte("* OK ready\r\n"))
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		tag, _, _ := strings.Cut(line, " ")
		server.Write([]byte("* 1 FETCH (UID 7 BODYSTRUCTURE " + structure + ")\r\n" + tag + " OK done\r\n"))
		r.ReadString('\n')
	}()
	c := imapclient.New(client, nil)
	msgs, err := c.Fetch(imap.UIDSetNum(7), &imap.FetchOptions{UID: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	mp := msgs[0].BodyStructure.(*imap.BodyStructureMultiPart)
	fwd := mp.Children[1].(*imap.BodyStructureSinglePart)
	if fwd.MessageRFC822 == nil || fwd.MessageRFC822.Envelope == nil || fwd.MessageRFC822.Envelope.Subject != "Fwd" || fwd.MessageRFC822.NumLines != 9 {
		t.Fatalf("the forwarded message %+v", fwd.MessageRFC822)
	}
	if !hasAttachment(msgs[0].BodyStructure) {
		t.Fatal("the forwarded message does not count as an attachment")
	}
}
