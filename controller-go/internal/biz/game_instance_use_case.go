package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"controller-go/internal/client/assetservice"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	assetservicev1 "controller-go/internal/third/assetservice/v1"
)

// GameInstanceUseCase 业务逻辑执行器
type GameInstanceUseCase struct {
	instanceRepo        repository.GameInstanceRepository
	ReconcileDispatcher *ReconcileDispatcher
	assetClient         *assetservice.AssetServiceFaceClient
}

func NewGameInstanceUseCase(
	instanceRepo repository.GameInstanceRepository,
	reconcileDispatcher *ReconcileDispatcher,
	assetClient *assetservice.AssetServiceFaceClient,
) *GameInstanceUseCase {
	return &GameInstanceUseCase{
		instanceRepo:        instanceRepo,
		ReconcileDispatcher: reconcileDispatcher,
		assetClient:         assetClient,
	}
}

/**
* 创建一个GameInstance，状态会被初始化为StatusStopped。
* buildID 为空时，以 "public" 作为 channel 调 asset_service 解析该 channel 的可用构建。
**/
func (uc *GameInstanceUseCase) CreateGameInstance(ctx context.Context, gameID, buildID string) (*entity.GameInstance, error) {
	if gameID == "" {
		return nil, errors.New("game_id is required")
	}

	build, err := uc.resolveBuild(ctx, gameID, buildID)
	if err != nil {
		return nil, err
	}

	instance := &entity.GameInstance{
		ID:              newGameInstanceID(),
		GameID:          gameID,
		Status:          entity.StatusStopped,
		GameBuildId:     build.GetBuildId(),
		LastPendingTime: time.Time{},
		CreateTime:      time.Now(),
		UpdateTime:      time.Now(),
	}
	err = uc.instanceRepo.Save(ctx, instance)
	if err != nil {
		return nil, err
	}
	return instance, nil
}

/**
* resolveBuild 解析实例要使用的 game_build：
* - buildID 非空：按 build_id 精确解析（"最新版本"语义由 asset_service 决定）
* - buildID 为空：以 "public" 作为 channel，取该 channel 的可用构建
* 两种情况都会校验返回的 build 确实属于 gameID。
**/
func (uc *GameInstanceUseCase) resolveBuild(ctx context.Context, gameID, buildID string) (*assetservicev1.GameBuild, error) {
	selector := &assetservicev1.VersionSelector{}
	if buildID != "" {
		selector.Selector = &assetservicev1.VersionSelector_BuildId{BuildId: buildID}
	} else {
		selector.Selector = &assetservicev1.VersionSelector_Channel{Channel: steamPublicBranchName}
	}

	resp, err := uc.assetClient.ResolveGameBuild(ctx, &assetservicev1.ResolveGameBuildRequest{
		Game:     &assetservicev1.Game{Id: gameID},
		Selector: selector,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Build == nil {
		return nil, errors.New("asset_service returned empty game build")
	}

	build := resp.Build
	if build.GetGame() == nil || build.GetGame().GetId() != gameID {
		return nil, fmt.Errorf("game build %q does not belong to game %q", build.GetBuildId(), gameID)
	}
	return build, nil
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
	if instance.Status != entity.StatusStopped &&
		instance.Status != entity.Failed {
		return fmt.Errorf("invalid instance status：%s",
			instance.Status)
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
	if instance.Status != entity.StatusRunning &&
		instance.Status != entity.Failed {
		return fmt.Errorf("invalid instance status：%s",
			instance.Status)
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
