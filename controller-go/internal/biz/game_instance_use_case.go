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
	instanceRepo            repository.GameInstanceRepository
	portMappingRepo         repository.ContainerPortMappingRepository
	nodeAgentRepo           repository.NodeAgentRepository
	nodeRepo                repository.NodeRepository
	gameRepo                repository.GameRepository
	gameContainerConfigRepo repository.GameContainerConfigRepository
	ReconcileDispatcher     *ReconcileDispatcher
	scheduler               Scheduler
	queueManager            *QueueManager
	assetClient             *assetservice.AssetServiceFaceClient
}

func NewGameInstanceUseCase(
	instanceRepo repository.GameInstanceRepository,
	portMappingRepo repository.ContainerPortMappingRepository,
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	gameRepo repository.GameRepository,
	gameContainerConfigRepo repository.GameContainerConfigRepository,
	reconcileDispatcher *ReconcileDispatcher,
	scheduler Scheduler,
	queueManager *QueueManager,
	assetClient *assetservice.AssetServiceFaceClient,
) *GameInstanceUseCase {
	return &GameInstanceUseCase{
		instanceRepo:            instanceRepo,
		portMappingRepo:         portMappingRepo,
		nodeAgentRepo:           nodeAgentRepo,
		nodeRepo:                nodeRepo,
		gameRepo:                gameRepo,
		gameContainerConfigRepo: gameContainerConfigRepo,
		ReconcileDispatcher:     reconcileDispatcher,
		scheduler:               scheduler,
		queueManager:            queueManager,
		assetClient:             assetClient,
	}
}

// CreateInstanceOptions 创建实例的可选参数（region/priority/资源显式覆盖）
type CreateInstanceOptions struct {
	GameBuildID string
	Region      string
	Priority    int // 0 = 默认 100
	Resources   *entity.ResourceRequest // 显式指定资源（覆盖 config 默认，ResourceOverride=true）
}

/**
* 创建一个GameInstance，状态会被初始化为StatusStopped。
* buildID 为空时，以 "public" 作为 channel 调 asset_service 解析该 channel 的可用构建。
**/
func (uc *GameInstanceUseCase) CreateGameInstance(ctx context.Context, gameID string, opts CreateInstanceOptions) (*entity.GameInstance, error) {
	if gameID == "" {
		return nil, errors.New("game_id is required")
	}

	build, err := uc.resolveBuild(ctx, gameID, opts.GameBuildID)
	if err != nil {
		return nil, err
	}

	priority := opts.Priority
	if priority <= 0 {
		priority = 100 // D7 默认
	}
	instance := &entity.GameInstance{
		ID:               newGameInstanceID(),
		GameID:           gameID,
		Status:           entity.StatusStopped,
		GameBuildId:      build.GetBuildId(),
		LastPendingTime:  time.Time{},
		CreateTime:       time.Now(),
		UpdateTime:       time.Now(),
		Region:           opts.Region,
		Priority:         priority,
		ResourceReq:      opts.Resources,
		ResourceOverride: opts.Resources != nil, // 创建时显式指定 → 覆盖 config 默认（000021）
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
	instance.FailReason = "" // 重新启动清失败原因
	instance.LastPendingTime = time.Now()
	// Save 全字段（确保 fail_reason 清除落库）
	err = uc.instanceRepo.Save(ctx, instance)
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
	instance.FailReason = "" // 重试清失败原因
	instance.LastPendingTime = time.Now()
	if err := uc.instanceRepo.Save(ctx, instance); err != nil {
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
* 先清理其端口映射，再删除实例记录。排队中（Queued）实例删除时联动出队（S38）。
**/
func (uc *GameInstanceUseCase) DeleteGameInstance(ctx context.Context, instanceID string) error {
	instance, err := uc.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		return err
	}
	if instance.Status == entity.StatusQueued {
		// 删除隐含取消排队（S38）
		if err := uc.queueManager.Cancel(ctx, instanceID); err != nil {
			return fmt.Errorf("cancel queue: %w", err)
		}
	} else if isDispatchableStatus(instance.Status) {
		return fmt.Errorf("instance is in dispatchable state %s, stop it first", instance.Status)
	}
	if err := uc.portMappingRepo.DeleteByInstanceId(ctx, instanceID); err != nil {
		return fmt.Errorf("delete instance port mappings: %w", err)
	}
	return uc.instanceRepo.Delete(ctx, instanceID)
}

/**
* 取消排队（D5）：移除出队，实例保持 stopped。仅 queued 状态允许（状态守卫）。
**/
func (uc *GameInstanceUseCase) CancelGameInstance(ctx context.Context, instanceID string) error {
	return uc.scheduler.CancelQueued(ctx, instanceID)
}

/**
* 查询实例已分配的端口映射
**/
func (uc *GameInstanceUseCase) GetInstancePorts(ctx context.Context, instanceID string) ([]*entity.ContainerPortMapping, error) {
	return uc.portMappingRepo.ListByInstanceId(ctx, instanceID)
}

// InstanceConnectInfo 实例对外连接信息（connect_address = node_ip:game_host_port）
type InstanceConnectInfo struct {
	NodeIP        string `json:"node_ip"`
	GameHostPort  uint16 `json:"game_host_port"`
	GamePort      uint16 `json:"game_port"` // 容器内游戏端口
	Protocol      string `json:"protocol"`  // tcp/udp
	HostPort      uint16 `json:"host_port"` // 映射的宿主端口（可能多个，取游戏端口那条）
	ContainerPort uint16 `json:"container_port"`
}

// GetInstanceConnectInfo 计算实例对客户端公开的连接地址：
// node_ip = 实例所在 node 的 IP；game_host_port = 游戏主端口(is_game_port)对应的宿主端口。
// 仅 running 实例有完整映射；未调度/无 node_agent 时返回 error。
func (uc *GameInstanceUseCase) GetInstanceConnectInfo(ctx context.Context, instanceID string) (*InstanceConnectInfo, error) {
	instance, err := uc.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if instance.NodeAgentID == nil {
		return nil, errors.New("instance has no node agent assigned")
	}
	nodeAgent, err := uc.nodeAgentRepo.GetByID(ctx, *instance.NodeAgentID)
	if err != nil {
		return nil, fmt.Errorf("load node agent: %w", err)
	}
	node, err := uc.nodeRepo.GetByID(nodeAgent.NodeId)
	if err != nil {
		return nil, fmt.Errorf("load node: %w", err)
	}
	game, err := uc.gameRepo.GetByID(ctx, instance.GameID)
	if err != nil {
		return nil, fmt.Errorf("load game: %w", err)
	}
	config, err := uc.gameContainerConfigRepo.GetByID(ctx, game.ContainerConfigID)
	if err != nil {
		return nil, fmt.Errorf("load container config: %w", err)
	}

	// 找游戏主端口（is_game_port 标记；没有标记则退化为第一个 excerpt 的起始端口）
	var gamePort uint16
	for _, e := range config.PortExcerpt {
		if e.IsGamePort {
			gamePort = e.BeginPort
			break
		}
	}
	if gamePort == 0 && len(config.PortExcerpt) > 0 {
		gamePort = config.PortExcerpt[0].BeginPort
	}
	if gamePort == 0 {
		return nil, errors.New("container config declares no game port")
	}

	mappings, err := uc.portMappingRepo.ListByInstanceId(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list port mappings: %w", err)
	}
	// 注入模式下 container_port == host_port（identity），无法按容器端口匹配，
	// 优先取 is_game_port 标记的映射；否则回退到容器端口 == 游戏端口。
	var fallback *entity.ContainerPortMapping
	for _, m := range mappings {
		if m.IsGamePort {
			return connectInfoFromMapping(node.Ip, gamePort, m), nil
		}
		if m.ContainerPort == gamePort && fallback == nil {
			fallback = m
		}
	}
	if fallback != nil {
		return connectInfoFromMapping(node.Ip, gamePort, fallback), nil
	}
	return nil, fmt.Errorf("no port mapping found for game port %d", gamePort)
}

// connectInfoFromMapping 由端口映射行构造连接信息
func connectInfoFromMapping(nodeIP string, gamePort uint16, m *entity.ContainerPortMapping) *InstanceConnectInfo {
	protocol := "tcp"
	if m.Protocol == entity.UDP {
		protocol = "udp"
	}
	return &InstanceConnectInfo{
		NodeIP:        nodeIP,
		GameHostPort:  m.HostPort,
		GamePort:      gamePort,
		Protocol:      protocol,
		HostPort:      m.HostPort,
		ContainerPort: m.ContainerPort,
	}
}
