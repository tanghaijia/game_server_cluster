package repository

import (
	"context"

	"platform-service/internal/entity"
)

// UserRepository 定义用户数据层必须实现的接口
type UserRepository interface {
	Save(ctx context.Context, user *entity.User) error
	GetByID(ctx context.Context, id string) (*entity.User, error)
	GetByUsername(ctx context.Context, username string) (*entity.User, error)
	ListAll(ctx context.Context) ([]*entity.User, error)
}
