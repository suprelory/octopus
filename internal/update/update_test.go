package update

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyReleaseArtifact(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	archive := []byte("signed archive")
	filename := "octopus-linux-x86_64.zip"
	digest := sha256.Sum256(archive)
	manifest := []byte(fmt.Sprintf("%x  %s\n", digest, filename))
	signature := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifest)) + "\n")
	encodedKey := base64.StdEncoding.EncodeToString(publicKey)

	if err := verifyReleaseArtifact(filename, archive, manifest, signature, encodedKey); err != nil {
		t.Fatalf("verifyReleaseArtifact rejected valid release: %v", err)
	}

	t.Run("tampered archive", func(t *testing.T) {
		if err := verifyReleaseArtifact(filename, []byte("tampered"), manifest, signature, encodedKey); err == nil {
			t.Fatal("tampered archive was accepted")
		}
	})
	t.Run("tampered manifest", func(t *testing.T) {
		tampered := append([]byte(nil), manifest...)
		tampered[0] ^= 1
		if err := verifyReleaseArtifact(filename, archive, tampered, signature, encodedKey); err == nil {
			t.Fatal("tampered manifest was accepted")
		}
	})
	t.Run("wrong signing key", func(t *testing.T) {
		otherPublic, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}
		if err := verifyReleaseArtifact(filename, archive, manifest, signature, base64.StdEncoding.EncodeToString(otherPublic)); err == nil {
			t.Fatal("signature from a different key was accepted")
		}
	})
	t.Run("missing embedded key", func(t *testing.T) {
		if err := verifyReleaseArtifact(filename, archive, manifest, signature, ""); err == nil {
			t.Fatal("release without embedded verification key was accepted")
		}
	})
}

func TestVerifyReleaseManifestBeforeArchiveDownload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	filename := "octopus-linux-x86_64.zip"
	digest := sha256.Sum256([]byte("archive"))
	manifest := []byte(fmt.Sprintf("%x  %s\n", digest, filename))
	signature := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifest)))

	expected, err := verifyReleaseManifest(filename, manifest, signature, base64.StdEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatalf("verifyReleaseManifest rejected valid data: %v", err)
	}
	if !bytes.Equal(expected, digest[:]) {
		t.Fatalf("verified digest = %x, want %x", expected, digest)
	}
	if _, err := verifyReleaseManifest(filename, manifest, []byte("invalid"), base64.StdEncoding.EncodeToString(publicKey)); err == nil {
		t.Fatal("verifyReleaseManifest accepted an invalid signature")
	}
}

func TestVerifyArtifactChecksumRejectsInvalidDigestLength(t *testing.T) {
	if err := verifyArtifactChecksum("octopus.zip", []byte("archive"), []byte("short")); err == nil {
		t.Fatal("verifyArtifactChecksum accepted an invalid expected digest length")
	}
}

func TestUpdateInProgressGuard(t *testing.T) {
	updateInProgress.Store(true)
	t.Cleanup(func() { updateInProgress.Store(false) })
	if err := UpdateCore(); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("UpdateCore concurrent guard error = %v", err)
	}
}

func TestChecksumForFileRejectsMalformedManifest(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{name: "invalid digest", manifest: "not-a-digest  octopus-linux-x86_64.zip\n"},
		{name: "path entry", manifest: strings.Repeat("0", 64) + "  nested/octopus-linux-x86_64.zip\n"},
		{name: "windows path entry", manifest: strings.Repeat("0", 64) + "  nested\\octopus-linux-x86_64.zip\n"},
		{name: "duplicate", manifest: strings.Repeat("0", 64) + "  octopus-linux-x86_64.zip\n" + strings.Repeat("1", 64) + "  octopus-linux-x86_64.zip\n"},
		{name: "missing", manifest: strings.Repeat("0", 64) + "  other.zip\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := checksumForFile([]byte(tt.manifest), "octopus-linux-x86_64.zip"); err == nil {
				t.Fatal("malformed checksum manifest was accepted")
			}
		})
	}
}

func TestReadAllLimit(t *testing.T) {
	data, err := readAllLimit(strings.NewReader("12345"), 5)
	if err != nil || string(data) != "12345" {
		t.Fatalf("readAllLimit exact limit = %q, %v", data, err)
	}
	if _, err := readAllLimit(strings.NewReader("123456"), 5); err == nil {
		t.Fatal("readAllLimit accepted an oversized response")
	}
}

func TestDoRequestRejectsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "release unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	if _, err := doRequest(server.URL, false, 1024); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("doRequest non-2xx error = %v", err)
	}
}

func TestDoRequestRejectsOversizedContentLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := doRequest(server.URL, false, 10); !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("doRequest oversized Content-Length error = %v, want %v", err, errResponseTooLarge)
	}
}

func TestExtractUpdateArchive(t *testing.T) {
	t.Run("corrupt zip", func(t *testing.T) {
		if _, err := extractUpdateArchive([]byte("not a zip"), t.TempDir(), "octopus"); err == nil {
			t.Fatal("corrupt update archive was accepted")
		}
	})

	t.Run("valid", func(t *testing.T) {
		archive := makeZip(t, []zipEntry{{name: "octopus", body: []byte("binary"), mode: 0755}})
		dest := t.TempDir()
		path, err := extractUpdateArchive(archive, dest, "octopus")
		if err != nil {
			t.Fatalf("extractUpdateArchive failed: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "binary" {
			t.Fatalf("extracted executable = %q, %v", data, err)
		}
	})

	for _, tt := range []struct {
		name    string
		entries []zipEntry
	}{
		{name: "path traversal", entries: []zipEntry{{name: "../octopus", body: []byte("bad"), mode: 0755}}},
		{name: "nested executable", entries: []zipEntry{{name: "bin/octopus", body: []byte("bad"), mode: 0755}}},
		{name: "missing executable", entries: []zipEntry{{name: "README.md", body: []byte("docs"), mode: 0644}}},
		{name: "symlink", entries: []zipEntry{{name: "octopus", body: []byte("target"), mode: os.ModeSymlink | 0777}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			archive := makeZip(t, tt.entries)
			if _, err := extractUpdateArchive(archive, t.TempDir(), "octopus"); err == nil {
				t.Fatal("unsafe update archive was accepted")
			}
		})
	}
}

func TestActivateExecutableRollsBackWhenActivationFails(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "octopus")
	if err := os.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatalf("write current executable: %v", err)
	}
	missingStaged := filepath.Join(dir, "missing")
	backupPath := execPath + ".rollback"
	if err := activateExecutable(missingStaged, execPath, backupPath); err == nil {
		t.Fatal("activateExecutable accepted a missing staged executable")
	}
	data, err := os.ReadFile(execPath)
	if err != nil || string(data) != "old" {
		t.Fatalf("current executable after failed install = %q, %v", data, err)
	}
}

func TestActivateExecutableKeepsRollbackCopy(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "octopus")
	stagedPath := filepath.Join(dir, "staged")
	if err := os.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatalf("write current executable: %v", err)
	}
	if err := os.WriteFile(stagedPath, []byte("new"), 0600); err != nil {
		t.Fatalf("write staged executable: %v", err)
	}
	backupPath := execPath + ".rollback"
	if err := activateExecutable(stagedPath, execPath, backupPath); err != nil {
		t.Fatalf("activateExecutable failed: %v", err)
	}
	newData, _ := os.ReadFile(execPath)
	oldData, _ := os.ReadFile(backupPath)
	if string(newData) != "new" || string(oldData) != "old" {
		t.Fatalf("installed=%q rollback=%q", newData, oldData)
	}
}

type zipEntry struct {
	name string
	body []byte
	mode os.FileMode
}

func makeZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		header.SetMode(entry.mode)
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("CreateHeader failed: %v", err)
		}
		if _, err := file.Write(entry.body); err != nil {
			t.Fatalf("write zip entry failed: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip failed: %v", err)
	}
	return buffer.Bytes()
}
