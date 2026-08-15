package biz

import (
	"strings"
	"testing"
	"time"
)

func TestFileSessionIssuer_Issue(t *testing.T) {
	issuer := NewFileSessionIssuer("test-secret", 30*time.Minute)
	token, exp, err := issuer.Issue("inst-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if !exp.After(time.Now()) {
		t.Error("expires_at should be in the future")
	}

	// JWT 结构：三段 base64
	if parts := strings.Split(token, "."); len(parts) != 3 {
		t.Errorf("expected 3 JWT parts, got %d", len(parts))
	}
}
