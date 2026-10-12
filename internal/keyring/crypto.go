package keyring

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"
)

// The session algorithms: secrets as they are, or encrypted with a key
// both sides derive by Diffie-Hellman (what libsecret asks for).
const (
	AlgPlain = "plain"
	AlgDH    = "dh-ietf1024-sha256-aes128-cbc-pkcs7"
)

// ietf1024 is the 1024-bit MODP group of RFC 2409 (the Second Oakley
// Group), generator 2.
var ietf1024, _ = new(big.Int).SetString(
	"FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74"+
		"020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F1437"+
		"4FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED"+
		"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE65381FFFFFFFFFFFFFFFF", 16)

// crypter seals and opens one session's secrets.
type crypter interface {
	// seal returns the parameters (the IV) and the value to send.
	seal(secret []byte) (params, value []byte, err error)
	open(params, value []byte) ([]byte, error)
}

type plain struct{}

func (plain) seal(secret []byte) ([]byte, []byte, error) { return nil, secret, nil }
func (plain) open(_, value []byte) ([]byte, error)       { return value, nil }

// dhKey is one side's key pair.
type dhKey struct{ priv, pub *big.Int }

func newDHKey() (dhKey, error) {
	b := make([]byte, 128)
	if _, err := rand.Read(b); err != nil {
		return dhKey{}, err
	}
	priv := new(big.Int).SetBytes(b)
	return dhKey{priv: priv, pub: new(big.Int).Exp(big.NewInt(2), priv, ietf1024)}, nil
}

// aesKey derives the session's AES-128 key from the peer's public key:
// the shared secret padded to the group's 128 bytes, through HKDF-SHA256
// without salt or info, as libsecret and gnome-keyring do.
func (k dhKey) aesKey(peer []byte) ([]byte, error) {
	p := new(big.Int).SetBytes(peer)
	if p.Cmp(big.NewInt(1)) <= 0 || p.Cmp(new(big.Int).Sub(ietf1024, big.NewInt(1))) >= 0 {
		return nil, errors.New("bad public key")
	}
	shared := new(big.Int).Exp(p, k.priv, ietf1024).FillBytes(make([]byte, 128))
	return hkdf.Key(sha256.New, shared, nil, "", 16)
}

type aesSession struct{ key []byte }

func (s aesSession) seal(secret []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, nil, err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, nil, err
	}
	pad := aes.BlockSize - len(secret)%aes.BlockSize
	data := append(bytes.Clone(secret), bytes.Repeat([]byte{byte(pad)}, pad)...)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(data, data)
	return iv, data, nil
}

func (s aesSession) open(iv, value []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	if len(iv) != aes.BlockSize || len(value) == 0 || len(value)%aes.BlockSize != 0 {
		return nil, errors.New("bad encrypted secret")
	}
	data := bytes.Clone(value)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(data, data)
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || !bytes.Equal(data[len(data)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return nil, errors.New("bad padding")
	}
	return data[:len(data)-pad], nil
}
