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
	portMappingRepo     repository.ContainerPortMappingRepository
	ReconcileDispatcher *ReconcileDispatcher
	assetClient         *assetservice.AssetServiceFaceClient
}

func NewGameInstanceUseCase(
	instanceRepo repository.GameInstanceRepository,
	portMappingRepo repository.ContainerPortMappingRepository,
	reconcileDispatcher *ReconcileDispatcher,
	assetClient *assetservice.AssetServiceFaceClient,
) *GameInstanceUseCase {
	return &GameInstanceUseCase{
		instanceRepo:        instanceRepo,
		portMappingRepo:     portMappingRepo,
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

/**
* 列出 GameInstance；status 非空时按状态过滤，为空时列出全部（按创建时间排序）
**/
func (uc *GameInstanceUseCase) ListGameInstances(ctx context.Context, status *entity.InstanceStatus) ([]*entity.GameInstance, error) {
	if status != nil {
		return uc.instanceRepo.ListByStatuses(ctx, *status)
	}
	return uc.instanceRepo.ListAll(ctx)
}

/**
* 重试失败的 GameInstance：状态置为 StatusPending 重新进入调度（仅 Failed 可重试）
**/
func (uc *GameInstanceUseCase) RetryGameInstance(ctx context.Context, instanceID string) error {
	instance, err := uc.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		return err
	}
	if instance.Status != entity.Failed {
		return fmt.Errorf("invalid instance status：%s（仅 failed 状态可重试）", instance.Status)
	}

	instance.Status = entity.StatusPending
	if err := uc.instanceRepo.UpdateStatus(ctx, instance); err != nil {
		return err
	}
	return uc.ReconcileDispatcher.RequestDispatch(ctx, instance)
}

/**
* ForceDispatch 跳过状态校验，将实例（当前状态原样）压入派遣队列。
* 用于调试：实例卡在中间态但队列未消费时强制重新调度。
**/
func (uc *GameInstanceUseCase) ForceDispatch(ctx context.Context, instanceID string) error {
	instance, err := uc.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		return err
	}
	return uc.ReconcileDispatcher.ForceDispatch(ctx, instance)
}

/**
* 删除 GameInstance（仅允许非调度中/非运行中的实例）：
* 先清理其端口映射，再删除实例记录。
**/
func (uc *GameInstanceUseCase) DeleteGameInstance(ctx context.Context, instanceID string) error {
	instance, err := uc.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		return err
	}
	if isDispatchableStatus(instance.Status) {
		return fmt.Errorf("instance is in dispatchable state %s, stop it first", instance.Status)
	}
	if err := uc.portMappingRepo.DeleteByInstanceId(ctx, instanceID); err != nil {
		return fmt.Errorf("delete instance port mappings: %w", err)
	}
	return uc.instanceRepo.Delete(ctx, instanceID)
}

/**
* 查询实例已分配的端口映射
**/
func (uc *GameInstanceUseCase) GetInstancePorts(ctx context.Context, instanceID string) ([]*entity.ContainerPortMapping, error) {
	return uc.portMappingRepo.ListByInstanceId(ctx, instanceID)
}
