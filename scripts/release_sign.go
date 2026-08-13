//go:build ignore

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const signingKeyEnv = "OCTOPUS_RELEASE_SIGNING_KEY"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: go run scripts/release_sign.go <generate|public-key|sign>")
	}
	switch args[0] {
	case "generate":
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		seed := privateKey.Seed()
		fmt.Printf("%s=%s\n", signingKeyEnv, base64.StdEncoding.EncodeToString(seed))
		fmt.Printf("OCTOPUS_RELEASE_PUBLIC_KEY=%s\n", base64.StdEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey)))
		return nil
	case "public-key":
		privateKey, err := loadPrivateKey()
		if err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey)))
		return nil
	case "sign":
		if len(args) != 3 {
			return errors.New("usage: go run scripts/release_sign.go sign <manifest> <signature>")
		}
		privateKey, err := loadPrivateKey()
		if err != nil {
			return err
		}
		manifest, err := os.ReadFile(args[1])
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		signature := ed25519.Sign(privateKey, manifest)
		if err := os.WriteFile(args[2], []byte(base64.StdEncoding.EncodeToString(signature)+"\n"), 0600); err != nil {
			return fmt.Errorf("write manifest signature: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func loadPrivateKey() (ed25519.PrivateKey, error) {
	encoded := strings.TrimSpace(os.Getenv(signingKeyEnv))
	if encoded == "" {
		return nil, fmt.Errorf("%s is required", signingKeyEnv)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", signingKeyEnv, err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("%s must contain a base64 Ed25519 seed or private key", signingKeyEnv)
	}
}
