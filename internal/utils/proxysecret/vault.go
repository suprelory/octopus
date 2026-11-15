// Package proxysecret gives proxy credentials their own key and namespace.
package proxysecret

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"

	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
)

func Open() (*checkinsecret.Vault, error) {
	if value := os.Getenv("OCTOPUS_PROXY_MASTER_KEY"); value != "" {
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(key) != 32 {
			return nil, errors.New("invalid proxy master key")
		}
		return checkinsecret.New(key)
	}
	path := os.Getenv("OCTOPUS_PROXY_KEY_FILE")
	if path == "" {
		path = filepath.Join("data", "proxy.key")
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
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(createErr) {
			key, err = os.ReadFile(path)
		} else if createErr != nil {
			return nil, createErr
		} else {
			_, writeErr := file.Write(key)
			err = errors.Join(writeErr, file.Sync(), file.Close())
		}
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("proxy master key must contain 32 bytes")
	}
	return checkinsecret.New(key)
}
