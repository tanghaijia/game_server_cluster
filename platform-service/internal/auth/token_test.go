package auth

import (
	"testing"
	"time"
)

func TestTokenManager_IssueAndParse(t *testing.T) {
	m := NewTokenManager("test-secret", 30*time.Minute, 7*24*time.Hour)

	access, refresh, err := m.Issue("user-1", 1)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	ac, err := m.Parse(access)
	if err != nil {
		t.Fatalf("parse access: %v", err)
	}
	if ac.UserID != "user-1" || ac.TokenType != TokenTypeAccess {
		t.Errorf("unexpected access claims: %+v", ac)
	}

	rc, err := m.Parse(refresh)
	if err != nil {
		t.Fatalf("parse refresh: %v", err)
	}
	if rc.TokenType != TokenTypeRefresh {
		t.Errorf("unexpected refresh claims: %+v", rc)
	}
}

func TestTokenManager_RejectTampered(t *testing.T) {
	m := NewTokenManager("test-secret", 30*time.Minute, 7*24*time.Hour)
	access, _, _ := m.Issue("user-1", 0)

	if _, err := m.Parse(access + "x"); err == nil {
		t.Error("tampered token should be rejected")
	}

	m2 := NewTokenManager("other-secret", 30*time.Minute, 7*24*time.Hour)
	if _, err := m2.Parse(access); err == nil {
		t.Error("token signed with different secret should be rejected")
	}
}
