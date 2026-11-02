package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStore_SetAndGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	ts, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	entry := &TokenEntry{
		ServerName:  "my-server",
		AccessToken: "access-abc",
		TokenType:   "Bearer",
		Scope:       "read write",
	}
	if err := ts.Set(entry); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := ts.Get("my-server")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.AccessToken != "access-abc" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "access-abc")
	}
	if got.Scope != "read write" {
		t.Errorf("Scope = %q, want %q", got.Scope, "read write")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestTokenStore_GetMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	ts, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	if got := ts.Get("nonexistent"); got != nil {
		t.Errorf("expected nil for missing server, got %+v", got)
	}
}

func TestTokenStore_Delete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	ts, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	_ = ts.Set(&TokenEntry{ServerName: "srv", AccessToken: "tok"})

	if err := ts.Delete("srv"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ts.Get("srv") != nil {
		t.Error("expected nil after delete")
	}
}

func TestTokenStore_List(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	ts, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	_ = ts.Set(&TokenEntry{ServerName: "a", AccessToken: "tok-a"})
	_ = ts.Set(&TokenEntry{ServerName: "b", AccessToken: "tok-b"})

	list := ts.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
}

func TestTokenStore_Persistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")

	ts1, _ := NewTokenStore(path)
	_ = ts1.Set(&TokenEntry{ServerName: "persistent", AccessToken: "persisted-tok"})

	// Open a second instance from the same file.
	ts2, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("second NewTokenStore: %v", err)
	}
	got := ts2.Get("persistent")
	if got == nil {
		t.Fatal("token not persisted")
	}
	if got.AccessToken != "persisted-tok" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "persisted-tok")
	}
}

func TestTokenStore_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	ts, _ := NewTokenStore(path)
	_ = ts.Set(&TokenEntry{ServerName: "x", AccessToken: "y"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("file perm = %o, want 0600", perm)
	}
}

func TestTokenEntry_IsExpired(t *testing.T) {
	past := &TokenEntry{ExpiresAt: time.Now().Add(-time.Hour)}
	if !past.IsExpired() {
		t.Error("token with past ExpiresAt should be expired")
	}

	future := &TokenEntry{ExpiresAt: time.Now().Add(time.Hour)}
	if future.IsExpired() {
		t.Error("token with future ExpiresAt should not be expired")
	}

	zero := &TokenEntry{} // zero ExpiresAt → never expires
	if zero.IsExpired() {
		t.Error("token with zero ExpiresAt should not be expired")
	}
}
