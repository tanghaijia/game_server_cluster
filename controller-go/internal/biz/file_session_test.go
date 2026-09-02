package biz

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

func TestFileSessionIssuer_IssueForAgent(t *testing.T) {
	issuer := NewFileSessionIssuer("test-secret", 30*time.Minute)
	token, exp, err := issuer.IssueForAgent("agent-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if !exp.After(time.Now()) {
		t.Error("expires_at should be in the future")
	}

	// scope 必须是 agent_logs（node_agent 端校验 scope）
	claims := parseClaimsForTest(t, token, "test-secret")
	if claims.Scope != agentLogsScope {
		t.Errorf("scope = %q, want %q", claims.Scope, agentLogsScope)
	}
	// agent 级不绑实例
	if claims.InstanceID != "" {
		t.Errorf("instance_id should be empty for agent token, got %q", claims.InstanceID)
	}
}

func TestFileSessionIssuer_ScopeDistinct(t *testing.T) {
	issuer := NewFileSessionIssuer("test-secret", 30*time.Minute)
	instToken, _, _ := issuer.Issue("inst-1")
	agentToken, _, _ := issuer.IssueForAgent("agent-1")

	instClaims := parseClaimsForTest(t, instToken, "test-secret")
	agentClaims := parseClaimsForTest(t, agentToken, "test-secret")
	if instClaims.Scope == agentClaims.Scope {
		t.Errorf("files 与 agent_logs scope 不应相同: %q", instClaims.Scope)
	}
	if instClaims.Scope != fileSessionScope {
		t.Errorf("files scope = %q, want %q", instClaims.Scope, fileSessionScope)
	}
	if agentClaims.Scope != agentLogsScope {
		t.Errorf("agent_logs scope = %q, want %q", agentClaims.Scope, agentLogsScope)
	}
}

// parseClaimsForTest 解码 token（HS256）并返回载荷，供 scope 断言
func parseClaimsForTest(t *testing.T, token, secret string) fileSessionClaims {
	t.Helper()
	parsed, err := jwt.ParseWithClaims(token, &fileSessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	claims, ok := parsed.Claims.(*fileSessionClaims)
	if !ok {
		t.Fatalf("unexpected claims type")
	}
	return *claims
}
