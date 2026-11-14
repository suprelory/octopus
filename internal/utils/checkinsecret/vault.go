// Package checkinsecret protects independently owned website credentials.
package checkinsecret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Vault struct {
	aead  cipher.AEAD
	keyID string
}

func New(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("checkin master key must contain 32 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(key)
	return &Vault{aead: a, keyID: hex.EncodeToString(h[:8])}, nil
}

func Open() (*Vault, error) {
	if value := os.Getenv("OCTOPUS_CHECKIN_MASTER_KEY"); value != "" {
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, errors.New("invalid base64 checkin master key")
		}
		return New(key)
	}
	path := os.Getenv("OCTOPUS_CHECKIN_KEY_FILE")
	if path == "" {
		path = filepath.Join("data", "checkin.key")
	}
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			key, err = os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			return New(key)
		}
		if err != nil {
			return nil, err
		}
		_, writeErr := file.Write(key)
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return New(key)
}

func (v *Vault) Seal(accountID string, plain []byte) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := v.aead.Seal(nonce, nonce, plain, []byte(accountID))
	return "v1:" + v.keyID + ":" + base64.StdEncoding.EncodeToString(sealed), nil
}

func (v *Vault) Unseal(accountID, value string) ([]byte, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != v.keyID {
		return nil, errors.New("checkin credential key does not match; restore the matching checkin.key")
	}
	data, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil || len(data) < v.aead.NonceSize() {
		return nil, errors.New("invalid encrypted checkin credential")
	}
	plain, err := v.aead.Open(nil, data[:v.aead.NonceSize()], data[v.aead.NonceSize():], []byte(accountID))
	if err != nil {
		return nil, errors.New("checkin credential could not be decrypted")
	}
	return plain, nil
}
