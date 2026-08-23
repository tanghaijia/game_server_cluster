package gorm

import (
	"context"
	"time"

	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type CredentialPoolRepo struct {
	db *gorm.DB
}

func NewCredentialPoolRepo(db *gorm.DB) *CredentialPoolRepo {
	return &CredentialPoolRepo{db: db}
}

func (r *CredentialPoolRepo) ListByGame(ctx context.Context, gameID, resourceType string) ([]entity.CredentialPool, error) {
	var out []entity.CredentialPool
	q := r.db.WithContext(ctx).Where("game_id = ?", gameID)
	if resourceType != "" {
		q = q.Where("resource_type = ?", resourceType)
	}
	err := q.Order("create_time DESC").Find(&out).Error
	return out, err
}

func (r *CredentialPoolRepo) GetByID(ctx context.Context, id string) (*entity.CredentialPool, error) {
	var cred entity.CredentialPool
	err := r.db.WithContext(ctx).First(&cred, "id = ?", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cred, nil
}

func (r *CredentialPoolRepo) Create(ctx context.Context, creds []entity.CredentialPool) error {
	if len(creds) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&creds).Error
}

func (r *CredentialPoolRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&entity.CredentialPool{}, "id = ?", id).Error
}

func (r *CredentialPoolRepo) Acquire(ctx context.Context, id, instanceID string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&entity.CredentialPool{}).
		Where("id = ? AND status = ?", id, entity.CredentialAvailable).
		Updates(map[string]any{
			"status":        entity.CredentialInUse,
			"instance_id":   instanceID,
			"allocated_at":  time.Now(),
			"released_at":   nil,
			"update_time":   time.Now(),
		})
	return res.RowsAffected > 0, res.Error
}

func (r *CredentialPoolRepo) Release(ctx context.Context, id, instanceID string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&entity.CredentialPool{}).
		Where("id = ? AND status = ? AND instance_id = ?", id, entity.CredentialInUse, instanceID).
		Updates(map[string]any{
			"status":           entity.CredentialAvailable,
			"instance_id":      nil,
			"last_instance_id": instanceID,
			"released_at":      time.Now(),
			"update_time":      time.Now(),
		})
	return res.RowsAffected > 0, res.Error
}

func (r *CredentialPoolRepo) ReleaseByInstance(ctx context.Context, instanceID string) error {
	return r.db.WithContext(ctx).Model(&entity.CredentialPool{}).
		Where("status = ? AND instance_id = ?", entity.CredentialInUse, instanceID).
		Updates(map[string]any{
			"status":           entity.CredentialAvailable,
			"instance_id":      nil,
			"last_instance_id": instanceID,
			"released_at":      time.Now(),
			"update_time":      time.Now(),
		}).Error
}

func (r *CredentialPoolRepo) FindAllocatedByInstance(ctx context.Context, gameID, resourceType, instanceID string) ([]entity.CredentialPool, error) {
	var out []entity.CredentialPool
	err := r.db.WithContext(ctx).
		Where("game_id = ? AND resource_type = ? AND status = ? AND instance_id = ?",
			gameID, resourceType, entity.CredentialInUse, instanceID).
		Find(&out).Error
	return out, err
}

func (r *CredentialPoolRepo) MarkOrphan(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&entity.CredentialPool{}).
		Where("id = ? AND status = ?", id, entity.CredentialInUse).
		Update("status", entity.CredentialOrphan).Error
}

func (r *CredentialPoolRepo) ForceRelease(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&entity.CredentialPool{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":           entity.CredentialAvailable,
			"instance_id":      nil,
			"released_at":      time.Now(),
			"update_time":      time.Now(),
		}).Error
}
