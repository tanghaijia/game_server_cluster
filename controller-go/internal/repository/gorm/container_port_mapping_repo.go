package gorm

import (
	"context"
	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type ContainerPortMappingRepo struct {
	db *gorm.DB
}

func NewContainerPortMappingRepo(db *gorm.DB) *ContainerPortMappingRepo {
	return &ContainerPortMappingRepo{db: db}
}

func (r *ContainerPortMappingRepo) Save(ctx context.Context, mapping *entity.ContainerPortMapping) error {
	return r.db.WithContext(ctx).Save(mapping).Error
}

func (r *ContainerPortMappingRepo) GetByID(ctx context.Context, id string) (*entity.ContainerPortMapping, error) {
	var mapping entity.ContainerPortMapping
	err := r.db.WithContext(ctx).First(&mapping, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &mapping, nil
}

func (r *ContainerPortMappingRepo) DeleteById(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&entity.ContainerPortMapping{}).Error
}

func (r *ContainerPortMappingRepo) ListByInstanceId(ctx context.Context, instanceId string) ([]*entity.ContainerPortMapping, error) {
	var mappings []*entity.ContainerPortMapping
	err := r.db.WithContext(ctx).Where("instance_id = ?", instanceId).Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *ContainerPortMappingRepo) ListByNodeAgentId(ctx context.Context, nodeAgentId string) ([]*entity.ContainerPortMapping, error) {
	var mappings []*entity.ContainerPortMapping
	err := r.db.WithContext(ctx).Where("node_agent_id = ?", nodeAgentId).Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *ContainerPortMappingRepo) DeleteByInstanceId(ctx context.Context, instanceId string) error {
	return r.db.WithContext(ctx).Where("instance_id = ?", instanceId).Delete(&entity.ContainerPortMapping{}).Error
}
