package biz

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fileSessionScope 文件会话 scope（与 node_agent file_server/auth.rs 一致）
const fileSessionScope = "files"

// agentLogsScope agent 日志会话 scope（与 node_agent file_server/auth.rs 一致，
// 见 docs/node-agent-logging-design.md §4.2）
const agentLogsScope = "agent_logs"

// FileSession 文件会话响应（与 node_agent 文件服务直连所需信息）
type FileSession struct {
	BaseURL    string    `json:"base_url"`
	Token      string    `json:"token"`
	InstanceID string    `json:"instance_id"`
	DataRoot   string    `json:"data_root"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// AgentLogSession agent 日志会话响应（P2，见 docs/node-agent-logging-design.md §4.2）
type AgentLogSession struct {
	BaseURL   string    `json:"base_url"`
	Token     string    `json:"token"`
	AgentID   string    `json:"agent_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// fileSessionClaims JWT 载荷：字段名与 node_agent 的 FileTokenClaims 对齐。
// agent_logs 会话复用同一结构，instance_id 置空。
type fileSessionClaims struct {
	InstanceID string `json:"instance_id"`
	Scope      string `json:"scope"`
	jwt.RegisteredClaims
}

// FileSessionIssuer 签发 node_agent 会话 JWT（HS256，共享密钥）。
// 实例文件会话（scope=files）与 agent 日志会话（scope=agent_logs）共用。
type FileSessionIssuer struct {
	secret []byte
	ttl    time.Duration
}

func NewFileSessionIssuer(secret string, ttl time.Duration) *FileSessionIssuer {
	return &FileSessionIssuer{secret: []byte(secret), ttl: ttl}
}

// Issue 为实例签发文件会话 token，返回 token 与过期时间
func (i *FileSessionIssuer) Issue(instanceID string) (string, time.Time, error) {
	expiresAt := time.Now().Add(i.ttl)
	claims := fileSessionClaims{
		InstanceID: instanceID,
		Scope:      fileSessionScope,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	return i.sign(claims, expiresAt)
}

// IssueForAgent 为 node_agent 签发日志会话 token（scope=agent_logs，instance_id 置空）。
// node_agent 端只校验 scope + 签名（agent 级，不绑实例）。
func (i *FileSessionIssuer) IssueForAgent(agentID string) (string, time.Time, error) {
	expiresAt := time.Now().Add(i.ttl)
	claims := fileSessionClaims{
		InstanceID: "", // agent 级：不绑定实例
		Scope:      agentLogsScope,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	_ = agentID // 预留：后续如需审计可将 agent 身份写入 token
	return i.sign(claims, expiresAt)
}

func (i *FileSessionIssuer) sign(claims fileSessionClaims, expiresAt time.Time) (string, time.Time, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}
