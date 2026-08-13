package gorm

import (
	"context"

	"platform-service/internal/entity"

	"gorm.io/gorm"
)

type UserRepo struct {
	db *gorm.DB
}

func NewUserRepo(db *gorm.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Save(ctx context.Context, user *entity.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*entity.User, error) {
	var user entity.User
	err := r.db.WithContext(ctx).First(&user, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*entity.User, error) {
	var user entity.User
	err := r.db.WithContext(ctx).First(&user, "username = ?", username).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepo) ListAll(ctx context.Context) ([]*entity.User, error) {
	var users []*entity.User
	err := r.db.WithContext(ctx).Order("create_time").Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}
