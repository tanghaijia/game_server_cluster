package biz

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fileSessionScope 文件会话 scope（与 node_agent file_server/auth.rs 一致）
const fileSessionScope = "files"

// FileSession 文件会话响应（与 node_agent 文件服务直连所需信息）
type FileSession struct {
	BaseURL    string    `json:"base_url"`
	Token      string    `json:"token"`
	InstanceID string    `json:"instance_id"`
	DataRoot   string    `json:"data_root"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// fileSessionClaims JWT 载荷：字段名与 node_agent 的 FileTokenClaims 对齐
type fileSessionClaims struct {
	InstanceID string `json:"instance_id"`
	Scope      string `json:"scope"`
	jwt.RegisteredClaims
}

// FileSessionIssuer 签发 node_agent 文件会话 JWT（HS256，共享密钥）
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
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}
