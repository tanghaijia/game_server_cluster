package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// Claims 自定义 JWT 载荷
type Claims struct {
	UserID    string `json:"user_id"`
	Role      int    `json:"role"`
	TokenType string `json:"token_type"` // access / refresh
	jwt.RegisteredClaims
}

// TokenManager 负责 JWT 的签发与校验（HS256）
type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// Issue 为用户签发 access + refresh 两个 token
func (m *TokenManager) Issue(userID string, role int) (accessToken, refreshToken string, err error) {
	now := time.Now()
	access, err := m.sign(Claims{
		UserID:    userID,
		Role:      role,
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
		},
	})
	if err != nil {
		return "", "", err
	}

	refresh, err := m.sign(Claims{
		UserID:    userID,
		Role:      role,
		TokenType: TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.refreshTTL)),
		},
	})
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func (m *TokenManager) sign(claims Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// Parse 校验 token 签名与有效期并返回载荷
func (m *TokenManager) Parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
