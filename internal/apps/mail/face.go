package mail

import (
	"hash/fnv"
	"strings"

	"github.com/lyricat/inua/internal/apps/pass"
)

// Tones is how many colours a sender's mark is drawn in.
const Tones = 6

// mailboxes are the domains anyone can have a mailbox at: a friend
// writing from gmail.com is not Google.
var mailboxes = map[string]bool{
	"gmail": true, "googlemail": true, "hotmail": true, "outlook": true,
	"live": true, "msn": true, "icloud": true, "me": true, "mac": true,
	"yahoo": true, "ymail": true, "aol": true, "proton": true, "protonmail": true,
	"pm": true, "fastmail": true, "gmx": true, "zoho": true, "yandex": true,
	"tutanota": true, "tuta": true, "qq": true, "163": true, "126": true,
	"foxmail": true, "naver": true, "daum": true, "hanmail": true,
}

// Face is how a sender with no photo is drawn: the logo of the service
// the address is at, when Phosphor draws one and the domain is not a
// mailbox anyone can have, else ""; and its tone, one of Tones, which
// the same address always gets.
func Face(a Address) (icon string, tone int) {
	addr := strings.ToLower(strings.TrimSpace(a.Addr))
	h := fnv.New32a()
	h.Write([]byte(addr))
	tone = int(h.Sum32() % Tones)
	_, domain, ok := strings.Cut(addr, "@")
	if !ok || domain == "" {
		return "", tone
	}
	for _, label := range strings.Split(domain, ".") {
		if mailboxes[label] {
			return "", tone
		}
	}
	return pass.Brand(domain), tone
}
