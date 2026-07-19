package biz

import (
	"context"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	"errors"
)

// GameInstanceUseCase 业务逻辑执行器
type GameInstanceUseCase struct {
	instanceRepo        repository.GameInstanceRepository
	ReconcileDispatcher *ReconcileDispatcher
}

func NewGameInstanceUseCase(instanceRepo repository.GameInstanceRepository, reconcileDispatcher *ReconcileDispatcher) *GameInstanceUseCase {
	return &GameInstanceUseCase{instanceRepo: instanceRepo, ReconcileDispatcher: reconcileDispatcher}
}

/**
* 创建一个GameInstance，状态会被初始化为StatusStopped
**/
func (uc *GameInstanceUseCase) CreateGameInstance(ctx context.Context, game *entity.Game) (*entity.GameInstance, error) {
	if game == nil {
		return nil, errors.New("game cannot be nil")
	}

	instance := &entity.GameInstance{
		ID:              "game-instance-1",
		Game:            game,
		NodeAgent:       nil,
		Status:          entity.StatusStopped,
		LastPendingTime: 0,
	}
	err := uc.instanceRepo.Save(ctx, instance)
	if err != nil {
		return nil, err
	}
	return instance, nil
}

/**
* 启动一个GameInstance，状态被设置为StatusPending
**/
func (uc *GameInstanceUseCase) StartGameInstance(ctx context.Context, instanceID string) error {
	instance, err := uc.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		return err
	}
	instance.Status = entity.StatusPending
	err = uc.instanceRepo.UpdateStatus(ctx, instance)
	if err != nil {
		return err
	}
	uc.ReconcileDispatcher.RequestDispatch(ctx, instance)
	return nil
}

/**
* 停止一个GameInstance，状态被设置为StatusStopping
**/
func (uc *GameInstanceUseCase) StopGameInstance(ctx context.Context, instanceID string) error {
	instance, err := uc.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		return err
	}
	instance.Status = entity.StatusStopping
	err = uc.instanceRepo.UpdateStatus(ctx, instance)
	if err != nil {
		return err
	}
	uc.ReconcileDispatcher.RequestDispatch(ctx, instance)
	return nil
}
