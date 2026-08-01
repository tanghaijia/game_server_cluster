package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
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
func (uc *GameInstanceUseCase) CreateGameInstance(ctx context.Context, gameID string) (*entity.GameInstance, error) {
	if gameID == "" {
		return nil, errors.New("game_id is required")
	}

	instance := &entity.GameInstance{
		ID:              newGameInstanceID(),
		GameID:          gameID,
		Status:          entity.StatusStopped,
		LastPendingTime: time.Time{},
		CreateTime:      time.Now(),
		UpdateTime:      time.Now(),
	}
	err := uc.instanceRepo.Save(ctx, instance)
	if err != nil {
		return nil, err
	}
	return instance, nil
}

// newGameInstanceID 生成唯一实例ID
func newGameInstanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("inst-%d", time.Now().UnixNano())
	}
	return "inst-" + hex.EncodeToString(b)
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

/**
* 获取一个GameInstance（含当前状态）
**/
func (uc *GameInstanceUseCase) GetGameInstance(ctx context.Context, instanceID string) (*entity.GameInstance, error) {
	return uc.instanceRepo.GetByID(ctx, instanceID)
}
