package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"platform-service/internal/entity"
	"platform-service/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// UserUseCase 用户业务逻辑
type UserUseCase struct {
	repo repository.UserRepository
}

func NewUserUseCase(repo repository.UserRepository) *UserUseCase {
	return &UserUseCase{repo: repo}
}

// CreateUser 创建用户：bcrypt 哈希密码后落库
func (uc *UserUseCase) CreateUser(ctx context.Context, username, password string) (*entity.User, error) {
	if username == "" {
		return nil, errors.New("username is required")
	}
	if len(password) < 6 {
		return nil, errors.New("password must be at least 6 characters")
	}

	if _, err := uc.repo.GetByUsername(ctx, username); err == nil {
		return nil, errors.New("username already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	now := time.Now()
	user := &entity.User{
		ID:           newEntityID("user"),
		Username:     username,
		PasswordHash: string(hash),
		Role:         entity.RoleUser,
		Status:       entity.UserStatusActive,
		CreateTime:   now,
		UpdateTime:   now,
	}
	if err := uc.repo.Save(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// GetUser 按 id 查询用户
func (uc *UserUseCase) GetUser(ctx context.Context, id string) (*entity.User, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	return uc.repo.GetByID(ctx, id)
}

// ListUsers 列出全部用户
func (uc *UserUseCase) ListUsers(ctx context.Context) ([]*entity.User, error) {
	return uc.repo.ListAll(ctx)
}

// VerifyPassword 校验用户名密码（供后续登录接口使用）
func (uc *UserUseCase) VerifyPassword(ctx context.Context, username, password string) (*entity.User, error) {
	user, err := uc.repo.GetByUsername(ctx, username)
	if err != nil {
		return nil, errors.New("invalid username or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid username or password")
	}
	return user, nil
}

// newEntityID 生成形如 "user-<hex>" 的稳定 ID
func newEntityID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}
