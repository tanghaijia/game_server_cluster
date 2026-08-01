package biz

import (
	"context"
	"controller-go/internal/client/assetservice"
	"controller-go/internal/client/nodeagent"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	assetservicev1 "controller-go/internal/third/assetservice/v1"
	nodeagentv1 "controller-go/internal/third/nodeagent/v1"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type ReconcileDispatcher struct {
	queue                   chan *entity.GameInstance
	instanceRepo            repository.GameInstanceRepository
	nodeAgnetRepo           repository.NodeAgentRepository
	nodeRepo                repository.NodeRepository
	scheduler               Scheduler
	nodeAgentClients        *nodeagent.ClientRegistry
	assetClient             *assetservice.AssetServiceFaceClient
	gameRepo                repository.GameRepository
	gameContainerConfigRepo repository.GameContainerConfigRepository
}

func NewReconcileDispatcher(
	instanceRepo repository.GameInstanceRepository,
	nodeAgnetRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	scheduler Scheduler,
	nodeAgentClients *nodeagent.ClientRegistry,
	assetClient *assetservice.AssetServiceFaceClient,
	gameRepo repository.GameRepository,
	gameContainerConfigRepo repository.GameContainerConfigRepository,
) *ReconcileDispatcher {
	return &ReconcileDispatcher{
		queue:                   make(chan *entity.GameInstance, 100),
		instanceRepo:            instanceRepo,
		nodeAgnetRepo:           nodeAgnetRepo,
		nodeRepo:                nodeRepo,
		scheduler:               scheduler,
		nodeAgentClients:        nodeAgentClients,
		assetClient:             assetClient,
		gameRepo:                gameRepo,
		gameContainerConfigRepo: gameContainerConfigRepo,
	}
}

/**
* 请求对一个GameInstance进行派遣
**/
func (d *ReconcileDispatcher) RequestDispatch(ctx context.Context, instance *entity.GameInstance) error {
	if instance == nil {
		return errors.New("instance cannot be nil")
	}

	if instance.Status == entity.StatusPending ||
		instance.Status == entity.StatusScheduling ||
		instance.Status == entity.StatusPreparingBuild ||
		instance.Status == entity.StatusRestoringSnapshot ||
		instance.Status == entity.StatusStarting ||
		instance.Status == entity.StatusStopping ||
		instance.Status == entity.StatusCleaning {
		d.queue <- instance
		return nil
	}

	return errors.New("instance is not in a dispatchable state")
}

/**
* 派遣下一个需要派遣的GameInstance, 若队列中为空则会阻塞
**/
func (d *ReconcileDispatcher) NextDispatch(ctx context.Context) error {
	instance := <-d.queue

	d.Dispatch(ctx, instance)

	return nil
}

func (d *ReconcileDispatcher) Dispatch(ctx context.Context, instance *entity.GameInstance) error {
	switch instance.Status {
	case entity.StatusPending:
		instance.Status = entity.StatusScheduling
		d.instanceRepo.UpdateStatus(ctx, instance)
		d.RequestDispatch(ctx, instance)
	case entity.StatusScheduling:
		node_agent_id, err := d.scheduler.Schedule(instance)
		if err != nil {
			instance.Status = entity.Failed
		} else {
			instance.Status = entity.StatusPreparingBuild
			instance.NodeAgentID = &node_agent_id
			d.RequestDispatch(ctx, instance)
		}
		d.instanceRepo.UpdateStatus(ctx, instance)
	case entity.StatusPreparingBuild:
		nodeAgent, err := d.nodeAgnetRepo.GetByID(ctx, *instance.NodeAgentID)
		if err != nil {
			slog.Error("[DB] nodeAgnetRepo GetByID fail", "NodeAgentId", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		node, err := d.nodeRepo.GetByID(nodeAgent.NodeId)
		if err != nil {
			slog.Error("[DB] nodeRepo GetByID fail", "NodeId", nodeAgent.NodeId)
			d.FailedInstance(ctx, instance)
			return nil
		}
		client, err := d.nodeAgentClients.Get(ctx, *instance.NodeAgentID, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
		if err != nil {
			slog.Error("[NodeAgentClients] Get Client fail", "NodeAgentID", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		req := &nodeagentv1.PrepareGameBuildRequest{
			BuildId: instance.GameBuildId,
		}
		resp, err := client.PrepareGameBuild(ctx, req)
		if err != nil {
			slog.Error("[NodeAgentClients] PrepareGameBuild fail",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
				"GameBuildId", instance.GameBuildId, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		go d.PollingResult(ctx, resp.Operation.OperationId, instance, client, d.onPrepareBuildSucceeded)
	case entity.StatusRestoringSnapshot:
		nodeAgent, err := d.nodeAgnetRepo.GetByID(ctx, *instance.NodeAgentID)
		if err != nil {
			slog.Error("[DB] nodeAgnetRepo GetByID fail", "NodeAgentId", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		node, err := d.nodeRepo.GetByID(nodeAgent.NodeId)
		if err != nil {
			slog.Error("[DB] nodeRepo GetByID fail", "NodeId", nodeAgent.NodeId)
			d.FailedInstance(ctx, instance)
			return nil
		}
		client, err := d.nodeAgentClients.Get(ctx, *instance.NodeAgentID, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
		if err != nil {
			slog.Error("[NodeAgentClients] Get Client fail", "NodeAgentID", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		snapshotResp, err := d.assetClient.GetLatestSnapshot(ctx, &assetservicev1.GetLatestSnapshotRequest{
			InstanceId: instance.ID,
		})
		if err != nil {
			slog.Error("[AssetService] GetLatestSnapshot fail",
				"instanceId", instance.ID, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		if snapshotResp.Snapshot == nil ||
			snapshotResp.Snapshot.Status != assetservicev1.SnapshotStatus_SNAPSHOT_STATUS_COMPLETED {
			slog.Info("[AssetService] instance 无可用 snapshot，视为全新实例",
				"instanceId", instance.ID,
			)
			instance.Status = entity.StatusStarting
			d.instanceRepo.UpdateStatus(ctx, instance)
			d.RequestDispatch(ctx, instance)
			return nil
		}
		restoreResp, err := client.RestoreSnapshot(ctx, &nodeagentv1.RestoreSnapshotRequest{
			InstanceId: instance.ID,
			SnapshotId: snapshotResp.Snapshot.SnapshotId,
		})
		if err != nil {
			slog.Error("[NodeAgentClients] RestoreSnapshot fail",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
				"SnapshotId", snapshotResp.Snapshot.SnapshotId, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		go d.PollingResult(ctx, restoreResp.Operation.OperationId, instance, client, d.onRestoreSnapshotSucceeded)
	case entity.StatusStarting:
		nodeAgent, err := d.nodeAgnetRepo.GetByID(ctx, *instance.NodeAgentID)
		if err != nil {
			slog.Error("[DB] nodeAgnetRepo GetByID fail", "NodeAgentId", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		node, err := d.nodeRepo.GetByID(nodeAgent.NodeId)
		if err != nil {
			slog.Error("[DB] nodeRepo GetByID fail", "NodeId", nodeAgent.NodeId)
			d.FailedInstance(ctx, instance)
			return nil
		}
		client, err := d.nodeAgentClients.Get(ctx, *instance.NodeAgentID, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
		if err != nil {
			slog.Error("[NodeAgentClients] Get Client fail", "NodeAgentID", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		// 从 asset_service 获取该实例对应的 game build
		buildResp, err := d.assetClient.GetGameBuild(ctx, &assetservicev1.GetGameBuildRequest{
			BuildId: instance.GameBuildId,
		})
		if err != nil {
			slog.Error("[AssetService] GetGameBuild fail",
				"instanceId", instance.ID, "GameBuildId", instance.GameBuildId, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		if buildResp.Build == nil || buildResp.Build.Game == nil {
			slog.Error("[AssetService] GetGameBuild 返回的 build 无效",
				"instanceId", instance.ID, "GameBuildId", instance.GameBuildId,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		// 通过 Game 找到该实例的容器配置
		game, err := d.gameRepo.GetByID(ctx, instance.GameID)
		if err != nil {
			slog.Error("[DB] gameRepo GetByID fail",
				"GameId", instance.GameID, "instanceId", instance.ID, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		containerConfig, err := d.gameContainerConfigRepo.GetByID(ctx, game.ContainerConfigID)
		if err != nil {
			slog.Error("[DB] gameContainerConfigRepo GetByID fail",
				"ContainerConfigID", game.ContainerConfigID, "instanceId", instance.ID, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		runtimeSpec := buildInstanceRuntimeSpec(instance, buildResp.Build, containerConfig)
		startResp, err := client.StartInstance(ctx, &nodeagentv1.StartInstanceRequest{Instance: runtimeSpec})
		if err != nil {
			slog.Error("[NodeAgentClients] StartInstance fail",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		go d.PollingResult(ctx, startResp.Operation.OperationId, instance, client, d.onStartInstanceSucceeded)
	case entity.StatusStopping:
		nodeAgent, err := d.nodeAgnetRepo.GetByID(ctx, *instance.NodeAgentID)
		if err != nil {
			slog.Error("[DB] nodeAgnetRepo GetByID fail", "NodeAgentId", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		node, err := d.nodeRepo.GetByID(nodeAgent.NodeId)
		if err != nil {
			slog.Error("[DB] nodeRepo GetByID fail", "NodeId", nodeAgent.NodeId)
			d.FailedInstance(ctx, instance)
			return nil
		}
		client, err := d.nodeAgentClients.Get(ctx, *instance.NodeAgentID, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
		if err != nil {
			slog.Error("[NodeAgentClients] Get Client fail", "NodeAgentID", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		stopResp, err := client.StopInstance(ctx, &nodeagentv1.StopInstanceRequest{
			InstanceId: instance.ID,
		})
		if err != nil {
			slog.Error("[NodeAgentClients] StopInstance fail",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		go d.PollingResult(ctx, stopResp.Operation.OperationId, instance, client, d.onStopInstanceSucceeded)
	case entity.StatusCleaning:
		nodeAgent, err := d.nodeAgnetRepo.GetByID(ctx, *instance.NodeAgentID)
		if err != nil {
			slog.Error("[DB] nodeAgnetRepo GetByID fail", "NodeAgentId", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		node, err := d.nodeRepo.GetByID(nodeAgent.NodeId)
		if err != nil {
			slog.Error("[DB] nodeRepo GetByID fail", "NodeId", nodeAgent.NodeId)
			d.FailedInstance(ctx, instance)
			return nil
		}
		client, err := d.nodeAgentClients.Get(ctx, *instance.NodeAgentID, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
		if err != nil {
			slog.Error("[NodeAgentClients] Get Client fail", "NodeAgentID", instance.NodeAgentID)
			d.FailedInstance(ctx, instance)
			return nil
		}
		cleanResp, err := client.CleanInstance(ctx, &nodeagentv1.CleanInstanceRequest{
			InstanceId: instance.ID,
		})
		if err != nil {
			slog.Error("[NodeAgentClients] CleanInstance fail",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return nil
		}
		go d.PollingResult(ctx, cleanResp.Operation.OperationId, instance, client, d.onCleanInstanceSucceeded)
	default:
		slog.Warn("无法被调度的状态", "status", instance.Status, "id", instance.ID)
		instance.Status = entity.Failed
		d.instanceRepo.UpdateStatus(ctx, instance)
	}
	return nil
}

func (d *ReconcileDispatcher) FailedInstance(ctx context.Context, instance *entity.GameInstance) {
	instance.Status = entity.Failed
	d.instanceRepo.UpdateStatus(ctx, instance)
}

/**
 * 开启一个轮询线程轮询操作结果，如果成功则请求下一步调度
 */
func (d *ReconcileDispatcher) PollingResult(ctx context.Context,
	operation_id string, instance *entity.GameInstance,
	client *nodeagent.NodeAgentFaceClient,
	onSucceeded func(ctx context.Context, instance *entity.GameInstance)) {
	time.Sleep(500 * time.Millisecond)
	deadLine := time.Now().Add(OPERATION_POLLING_MINITE * time.Minute)
	for time.Now().Before(deadLine) {
		req := &nodeagentv1.GetOperationRequest{
			OperationId: operation_id,
		}
		resp, err := client.GetOperation(ctx, req)
		if err != nil {
			slog.Error("[NodeAgentClients] GetOperation fail",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
				"OperationId", operation_id, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return
		}
		if resp.Operation.Status == nodeagentv1.OperationStatus_OPERATION_STATUS_FAILED {
			slog.Error("[NodeAgentClients] GetOperation fail",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
				"OperationId", operation_id,
				"Message", resp.Operation.Message,
			)
			d.FailedInstance(ctx, instance)
			return
		}
		if resp.Operation.Status == nodeagentv1.OperationStatus_OPERATION_STATUS_SUCCEEDED {
			slog.Info("[NodeAgentClients] GetOperation success",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
				"OperationId", operation_id,
			)
			onSucceeded(ctx, instance)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// 轮询超时
	slog.Error("[NodeAgentClients] GetOperation polling time out",
		"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
		"OperationId", operation_id,
	)
	d.FailedInstance(ctx, instance)
}

/**
* PrepareGameBuild 成功后的回调：推进到还原快照阶段
**/
func (d *ReconcileDispatcher) onPrepareBuildSucceeded(ctx context.Context, instance *entity.GameInstance) {
	instance.Status = entity.StatusRestoringSnapshot
	d.RequestDispatch(ctx, instance)
}

/**
* RestoreSnapshot 成功后的回调：进入启动流程（start_instance 由 StatusStarting 分支处理）
**/
func (d *ReconcileDispatcher) onRestoreSnapshotSucceeded(ctx context.Context, instance *entity.GameInstance) {
	instance.Status = entity.StatusStarting
	d.instanceRepo.UpdateStatus(ctx, instance)
	d.RequestDispatch(ctx, instance)
}

/**
* StartInstance 成功后的回调：实例真正进入运行状态（终态）
**/
func (d *ReconcileDispatcher) onStartInstanceSucceeded(ctx context.Context, instance *entity.GameInstance) {
	instance.Status = entity.StatusRunning
	d.instanceRepo.UpdateStatus(ctx, instance)
}

/**
* StopInstance 成功后的回调：进入清理流程（clean_instance 由 StatusCleaning 分支处理）
**/
func (d *ReconcileDispatcher) onStopInstanceSucceeded(ctx context.Context, instance *entity.GameInstance) {
	instance.Status = entity.StatusCleaning
	d.instanceRepo.UpdateStatus(ctx, instance)
	d.RequestDispatch(ctx, instance)
}

/**
* CleanInstance 成功后的回调：实例完全停止（终态）
**/
func (d *ReconcileDispatcher) onCleanInstanceSucceeded(ctx context.Context, instance *entity.GameInstance) {
	instance.Status = entity.StatusStopped
	d.instanceRepo.UpdateStatus(ctx, instance)
}

/**
* 构造 start_instance 所需的 InstanceRuntimeSpec
**/
func buildInstanceRuntimeSpec(
	instance *entity.GameInstance,
	build *assetservicev1.GameBuild,
	config *entity.GameContainerConfig,
) *nodeagentv1.InstanceRuntimeSpec {
	return &nodeagentv1.InstanceRuntimeSpec{
		InstanceId:          instance.ID,
		Build:               mapGameBuild(build),
		ContainerServerPath: config.ContainerServerPath,
		PortMapping:         mapPortMapping(config),
		// spec 目前无数据来源，先填占位结构体以满足 nodeagent 的校验
		Spec: &nodeagentv1.InstanceSpec{
			Resources: &nodeagentv1.ResourceRequirements{},
		},
	}
}

/**
* 将 asset_service 的 GameBuild 映射为 nodeagent 的 GameBuild
**/
func mapGameBuild(build *assetservicev1.GameBuild) *nodeagentv1.GameBuild {
	if build == nil {
		return nil
	}
	game := &nodeagentv1.Game{}
	if build.Game != nil {
		game = &nodeagentv1.Game{
			Id:    build.Game.Id,
			Name:  build.Game.Name,
			AppId: build.Game.AppId,
		}
	}
	return &nodeagentv1.GameBuild{
		BuildId:           build.BuildId,
		Game:              game,
		Channel:           build.Channel,
		AdapterVersion:    build.AdapterVersion,
		UpstreamVersion:   build.UpstreamVersion,
		ArtifactUri:       build.ArtifactUri,
		ArtifactImageName: build.ArtifactImageName,
		ArtifactImageTag:  build.ArtifactImageTag,
	}
}

/**
* 将 GameContainerConfig 的端口映射映射为 nodeagent 的 PortMapping
**/
func mapPortMapping(config *entity.GameContainerConfig) *nodeagentv1.PortMapping {
	pm := &nodeagentv1.PortMapping{}
	switch config.PortMode {
	case entity.PORT_MAPPING_MOD_HOST:
		pm.Mode = nodeagentv1.PortMappingMod_PORT_MAPPING_MOD_HOST
	default:
		pm.Mode = nodeagentv1.PortMappingMod_PORT_MAPPING_MOD_NAT
	}
	for _, m := range config.PortMapping {
		entry := &nodeagentv1.PortMapEntry{
			HostPort:      uint32(m.HostPort),
			ContainerPort: uint32(m.ContainerPort),
		}
		switch m.Protocol {
		case entity.UDP:
			entry.Protocol = nodeagentv1.MappingPortProtocol_MAPPING_PORT_PROTOCOL_UDP
		default:
			entry.Protocol = nodeagentv1.MappingPortProtocol_MAPPING_PORT_PROTOCOL_TCP
		}
		pm.Entries = append(pm.Entries, entry)
	}
	return pm
}

/**
* 开启一个goroutine来处理派遣队列中的GameInstance
**/
func (d *ReconcileDispatcher) Start(ctx context.Context) {
	go func() {
		for {
			// 处理派遣逻辑
			d.NextDispatch(ctx)
		}
	}()
}
