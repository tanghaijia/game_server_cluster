package repository

import (
	"context"
	"controller-go/internal/entity"
)

type ContainerPortMappingRepository interface {
	Save(ctx context.Context, mapping *entity.ContainerPortMapping) error
	GetByID(ctx context.Context, id string) (*entity.ContainerPortMapping, error)
	DeleteById(ctx context.Context, id string) error
	ListByInstanceId(ctx context.Context, instanceId string) ([]*entity.ContainerPortMapping, error)
	ListByNodeAgentId(ctx context.Context, nodeAgentId string) ([]*entity.ContainerPortMapping, error)
	DeleteByInstanceId(ctx context.Context, instanceId string) error
}
