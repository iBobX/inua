package gamestream

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// What pairing can end with, past the network's errors.
var (
	ErrWrongPIN       = errors.New("gamestream: the PIN typed on the host is not this one")
	ErrPairing        = errors.New("gamestream: the host is pairing with another device")
	ErrPairingRefused = errors.New("gamestream: the host refused to pair")
	ErrImpostor       = errors.New("gamestream: the host's answer was not signed by its certificate")
)

// NewPIN is a PIN for the host's user to type: four digits.
func NewPIN() string {
	var b [2]byte
	rand.Read(b[:])
	return fmt.Sprintf("%04d", (int(b[0])<<8|int(b[1]))%10000)
}

// pairStage is the answer to a pairing request.
type pairStage struct {
	Paired    int    `xml:"paired"`
	PlainCert string `xml:"plaincert"`
	Challenge string `xml:"challengeresponse"`
	Secret    string `xml:"pairingsecret"`
}

// Pair pairs with the host, whose user types pin on it. The first request
// waits until they do (or ctx ends). On success the host's certificate is
// pinned in h.Cert; on failure the host is told to forget the attempt.
//
// Both sides prove they know the PIN without sending it: each encrypts a
// challenge with a key made from the PIN, and signs a secret with its own
// certificate, which the other checks.
func (h *Host) Pair(ctx context.Context, pin, deviceName string) (err error) {
	var salt [16]byte
	rand.Read(salt[:])
	sum := sha256.Sum256(append(salt[:], pin...))
	key := sum[:16]

	ask := func(args url.Values, secure bool, timeout time.Duration) (pairStage, error) {
		args.Set("devicename", deviceName)
		args.Set("updateState", "1")
		var st pairStage
		if err := h.get(ctx, secure, timeout, "pair", args, &st); err != nil {
			return st, err
		}
		if st.Paired != 1 {
			return st, ErrPairingRefused
		}
		return st, nil
	}
	defer func() {
		h.mu.Lock()
		h.pairing = nil
		h.mu.Unlock()
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			h.get(cleanup, false, requestTimeout, "unpair", nil, nil)
			cancel()
		}
	}()

	// 1. This client's certificate, for the host's; the host waits for the PIN.
	st, err := ask(url.Values{"phrase": {"getservercert"}, "salt": {hex.EncodeToString(salt[:])},
		"clientcert": {hex.EncodeToString(h.ID.CertPEM)}}, false, 0)
	if err != nil {
		return err
	}
	if st.PlainCert == "" {
		return ErrPairing
	}
	certPEM, err := hex.DecodeString(st.PlainCert)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("gamestream: the host's certificate is not PEM")
	}
	serverCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.pairing = certPEM
	h.mu.Unlock()

	// 2. A challenge, encrypted with the PIN's key; the host answers its hash and a challenge of its own.
	var challenge [16]byte
	rand.Read(challenge[:])
	st, err = ask(url.Values{"clientchallenge": {hex.EncodeToString(ecb(key, challenge[:], true))}}, false, requestTimeout)
	if err != nil {
		return err
	}
	resp, err := hex.DecodeString(st.Challenge)
	if err != nil || len(resp) < 48 || len(resp)%16 != 0 {
		return errors.New("gamestream: a malformed challenge response")
	}
	resp = ecb(key, resp, false)
	serverHash, serverChallenge := resp[:32], resp[32:48]

	// 3. The hash of the host's challenge, this certificate's signature and a secret.
	var secret [16]byte
	rand.Read(secret[:])
	mine := sha256.Sum256(concat(serverChallenge, h.ID.cert.Signature, secret[:]))
	st, err = ask(url.Values{"serverchallengeresp": {hex.EncodeToString(ecb(key, mine[:], true))}}, false, requestTimeout)
	if err != nil {
		return err
	}
	pairing, err := hex.DecodeString(st.Secret)
	if err != nil || len(pairing) <= 16 {
		return errors.New("gamestream: a malformed pairing secret")
	}
	serverSecret, serverSig := pairing[:16], pairing[16:]
	digest := sha256.Sum256(serverSecret)
	pub, ok := serverCert.PublicKey.(*rsa.PublicKey)
	if !ok || rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], serverSig) != nil {
		return ErrImpostor
	}
	expect := sha256.Sum256(concat(challenge[:], serverCert.Signature, serverSecret))
	if !bytes.Equal(expect[:], serverHash) {
		return ErrWrongPIN
	}

	// 4. This client's secret, signed.
	digest = sha256.Sum256(secret[:])
	sig, err := rsa.SignPKCS1v15(rand.Reader, h.ID.key, crypto.SHA256, digest[:])
	if err != nil {
		return err
	}
	if _, err := ask(url.Values{"clientpairingsecret": {hex.EncodeToString(concat(secret[:], sig))}}, false, requestTimeout); err != nil {
		return err
	}

	// 5. Over HTTPS, with this client's certificate: the host now knows it.
	if _, err := ask(url.Values{"phrase": {"pairchallenge"}}, true, requestTimeout); err != nil {
		return err
	}
	h.Cert = certPEM
	return nil
}

// Unpair asks the host to forget this client.
func (h *Host) Unpair(ctx context.Context) error {
	return h.get(ctx, false, requestTimeout, "unpair", nil, nil)
}

// ecb is AES-128 in ECB mode without padding, as pairing uses it.
func ecb(key, data []byte, encrypt bool) []byte {
	b, _ := aes.NewCipher(key)
	out := make([]byte, len(data))
	for i := 0; i+16 <= len(data); i += 16 {
		if encrypt {
			b.Encrypt(out[i:], data[i:i+16])
		} else {
			b.Decrypt(out[i:], data[i:i+16])
		}
	}
	return out
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
