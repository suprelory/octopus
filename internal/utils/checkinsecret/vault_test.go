package checkinsecret

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultAuthenticatesCiphertextAndAccountIdentity(t *testing.T) {
	v, err := New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	a, err := v.Seal("account-a", []byte("Cookie session=secret"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.Seal("account-a", []byte("Cookie session=secret"))
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("nonce reused")
	}
	if _, err := v.Unseal("account-b", a); err == nil {
		t.Fatal("ciphertext transferred to another account")
	}
	other, _ := New(bytes.Repeat([]byte{2}, 32))
	if _, err := other.Unseal("account-a", a); err == nil {
		t.Fatal("wrong master key accepted")
	}
	plain, err := v.Unseal("account-a", a)
	if err != nil || string(plain) != "Cookie session=secret" {
		t.Fatal("round trip failed")
	}
	broken := a[:len(a)-4] + "AAAA"
	if _, err := v.Unseal("account-a", broken); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestMasterKeyPersistsAndEnvironmentTakesPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", "")
	t.Setenv("OCTOPUS_CHECKIN_KEY_FILE", path)
	first, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := first.Seal("account", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	next, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Unseal("account", cipher); err != nil {
		t.Fatal("key changed after restart")
	}
	key, err := os.ReadFile(path)
	if err != nil || len(key) != 32 {
		t.Fatal("invalid persisted key")
	}
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	override, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := override.Unseal("account", cipher); err == nil {
		t.Fatal("file overrode configured environment key")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(key, after) {
		t.Fatal("environment override replaced the file")
	}
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", "malformed")
	if _, err := Open(); err == nil {
		t.Fatal("malformed key silently replaced")
	}
}
