package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	content := []byte("hello-agent")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])

	if err := VerifyChecksum(path, hexSum); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	if err := VerifyChecksum(path, "deadbeef"); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestAtomicSwap(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agent")
	newer := filepath.Join(dir, "agent.new")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicSwap(newer, target); err != nil {
		t.Fatalf("AtomicSwap: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("got %q, want new", string(data))
	}
}

func TestSmokeTest_MissingBinary(t *testing.T) {
	err := SmokeTest(context.Background(), filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected smoke test failure")
	}
}

func TestSmokeTest_BadBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad")
	if err := os.WriteFile(path, []byte("not-a-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	err := SmokeTest(context.Background(), path)
	if err == nil {
		t.Fatal("expected smoke test failure for non-executable content")
	}
}
