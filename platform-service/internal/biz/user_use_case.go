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

// GetUserByName 按用户名查询用户（用于管理员播种等）
func (uc *UserUseCase) GetUserByName(ctx context.Context, username string) (*entity.User, error) {
	return uc.repo.GetByUsername(ctx, username)
}

// CreateAdmin 创建管理员（播种用）；密码缺失时使用默认值
func (uc *UserUseCase) CreateAdmin(ctx context.Context, username, password string) (*entity.User, error) {
	if password == "" {
		password = "admin123"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	now := time.Now()
	admin := &entity.User{
		ID:           newEntityID("user"),
		Username:     username,
		PasswordHash: string(hash),
		Role:         entity.RoleAdmin,
		Status:       entity.UserStatusActive,
		CreateTime:   now,
		UpdateTime:   now,
	}
	if err := uc.repo.Save(ctx, admin); err != nil {
		return nil, err
	}
	return admin, nil
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

// SetRole 修改用户角色（仅管理员调用）
func (uc *UserUseCase) SetRole(ctx context.Context, id string, role entity.UserRole) (*entity.User, error) {
	user, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	user.Role = role
	user.UpdateTime = time.Now()
	if err := uc.repo.Save(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// SetStatus 启用/禁用用户（仅管理员调用）
func (uc *UserUseCase) SetStatus(ctx context.Context, id string, status entity.UserStatus) (*entity.User, error) {
	user, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	user.Status = status
	user.UpdateTime = time.Now()
	if err := uc.repo.Save(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
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
