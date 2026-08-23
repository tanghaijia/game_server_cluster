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
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReconcileDispatcher struct {
	queue                   chan *entity.GameInstance
	instanceRepo            repository.GameInstanceRepository
	nodeAgnetRepo           repository.NodeAgentRepository
	nodeRepo                repository.NodeRepository
	scheduler               Scheduler
	reservationRepo         repository.ReservationRepository
	queueManager            *QueueManager
	eventBus                *SchedulerEventBus
	nodeAgentClients        *nodeagent.ClientRegistry
	assetClient             *assetservice.AssetServiceFaceClient
	gameRepo                repository.GameRepository
	gameContainerConfigRepo repository.GameContainerConfigRepository
	gameContainerPortMapper GameContainerPortMapper
	// 平台运营方配置（M5）：启动实例时与 player 配置合并下发
	platformConfigRepo repository.GamePlatformConfigRepository
	// 外部受限凭证池（M8）：StatusStarting 分配 / 停止或失败释放
	credentialUC *CredentialUseCase

	// 自动重试计数(进程内):instanceID -> 连续重试次数,成功后清零
	retryMu              sync.Mutex
	operationRetryCounts map[string]int

	// 资源释放事件回调（S14：实例停止释放资源后唤醒排队）
	resourceReleasedHook func()

	// B-12：单条 node_agent/asset_service RPC 超时。reconcile 消费端是单 goroutine，
	// 一条挂起的 RPC 会冻结整条管线；所有同步 RPC 均以 withRPCTimeout 包裹。
	rpcTimeout time.Duration

	// B-13：派发队列满时的溢出缓冲（非阻塞入队兜底）。
	// 消费端优先取 overflow，再取 queue；避免"队列满 + 消费端自入队"自死锁。
	overflowMu sync.Mutex
	overflow   []*entity.GameInstance

	// B-14：节点失联 fencing。Running 实例在其节点被健康监控判为 unhealthy 且
	// InspectInstance 持续不可达超过 fencingTimeout 后，置 Failed 释放槽位/凭证/预留。
	offlineMu      sync.Mutex
	offlineSince   map[string]time.Time // instanceID -> 首次观测到"节点不可达"的时间
	fencingTimeout time.Duration
}

func NewReconcileDispatcher(
	instanceRepo repository.GameInstanceRepository,
	nodeAgnetRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	scheduler Scheduler,
	reservationRepo repository.ReservationRepository,
	queueManager *QueueManager,
	eventBus *SchedulerEventBus,
	nodeAgentClients *nodeagent.ClientRegistry,
	assetClient *assetservice.AssetServiceFaceClient,
	gameRepo repository.GameRepository,
	gameContainerConfigRepo repository.GameContainerConfigRepository,
	gameContainerPortMapper GameContainerPortMapper,
	platformConfigRepo repository.GamePlatformConfigRepository,
	credentialUC *CredentialUseCase,
	rpcTimeout time.Duration,
	fencingTimeout time.Duration,
) *ReconcileDispatcher {
	if rpcTimeout <= 0 {
		rpcTimeout = 30 * time.Second
	}
	if fencingTimeout <= 0 {
		fencingTimeout = 3 * time.Minute
	}
	return &ReconcileDispatcher{
		queue:                   make(chan *entity.GameInstance, 100),
		instanceRepo:            instanceRepo,
		nodeAgnetRepo:           nodeAgnetRepo,
		nodeRepo:                nodeRepo,
		scheduler:               scheduler,
		reservationRepo:         reservationRepo,
		queueManager:            queueManager,
		eventBus:                eventBus,
		nodeAgentClients:        nodeAgentClients,
		assetClient:             assetClient,
		gameRepo:                gameRepo,
		gameContainerConfigRepo: gameContainerConfigRepo,
		gameContainerPortMapper: gameContainerPortMapper,
		platformConfigRepo:      platformConfigRepo,
		credentialUC:            credentialUC,
		operationRetryCounts:    make(map[string]int),
		rpcTimeout:              rpcTimeout,
		overflow:                make([]*entity.GameInstance, 0, 64),
		offlineSince:            make(map[string]time.Time),
		fencingTimeout:          fencingTimeout,
	}
}

// dispatchableStatuses 是需要持续调度的中间态（未完成生命周期的实例）
var dispatchableStatuses = []entity.InstanceStatus{
	entity.StatusPending,
	entity.StatusScheduling,
	entity.StatusQueued,
	entity.StatusPreparingBuild,
	entity.StatusRestoringSnapshot,
	entity.StatusStarting,
	entity.StatusStopping,
	entity.StatusCleaning,
}

// isDispatchableStatus 判断实例状态是否处于需要调度的中间态
func isDispatchableStatus(status entity.InstanceStatus) bool {
	for _, s := range dispatchableStatuses {
		if status == s {
			return true
		}
	}
	return false
}

/**
* 请求对一个GameInstance进行派遣
*
* B-13（P0）：非阻塞入队。队列满时落入 overflow 溢出缓冲（而非阻塞发送），
* 消费端在取队列前先取 overflow。这消除了"队列打满 + 消费端处理中自入队"的自死锁：
* 消费端（唯一 drainer）永远不会因为向自己的队列塞回实例而把自己堵死。
* 实例不会丢失：overflow 由消费端在下一轮消费。
**/
func (d *ReconcileDispatcher) RequestDispatch(ctx context.Context, instance *entity.GameInstance) error {
	if instance == nil {
		return errors.New("instance cannot be nil")
	}

	if isDispatchableStatus(instance.Status) {
		select {
		case d.queue <- instance:
			return nil
		default:
			// 队列满：非阻塞落入 overflow（消费端优先取 overflow）
			d.overflowMu.Lock()
			d.overflow = append(d.overflow, instance)
			d.overflowMu.Unlock()
			return nil
		}
	}

	return errors.New("instance is not in a dispatchable state")
}

/**
* ForceDispatch 跳过状态校验，直接将实例压入派遣队列。
* 用于调试：当实例状态因异常停留在可调度中间态但未被消费时，手动强制重新入队。
**/
func (d *ReconcileDispatcher) ForceDispatch(ctx context.Context, instance *entity.GameInstance) error {
	if instance == nil {
		return errors.New("instance cannot be nil")
	}
	d.queue <- instance
	return nil
}

// QueueLen 返回当前派遣队列长度（调试用；含 overflow 溢出缓冲）
func (d *ReconcileDispatcher) QueueLen() int {
	d.overflowMu.Lock()
	ov := len(d.overflow)
	d.overflowMu.Unlock()
	return len(d.queue) + ov
}

// RetryCounts 返回各实例当前自动重试次数快照（调试用）
func (d *ReconcileDispatcher) RetryCounts() map[string]int {
	d.retryMu.Lock()
	defer d.retryMu.Unlock()
	out := make(map[string]int, len(d.operationRetryCounts))
	for k, v := range d.operationRetryCounts {
		out[k] = v
	}
	return out
}

/**
* 派遣下一个需要派遣的GameInstance, 若队列与溢出缓冲均为空则会阻塞。
* ctx 取消时返回 ctx.Err()，配合 Start 让消费 goroutine 优雅退出（B-12/B-17 相关）。
**/
func (d *ReconcileDispatcher) NextDispatch(ctx context.Context) error {
	instance := d.nextDispatchInstance(ctx)
	if instance == nil {
		return ctx.Err()
	}

	d.Dispatch(ctx, instance)

	return nil
}

// nextDispatchInstance 取下一个待派遣实例：优先取 overflow（满队列兜底），
// 否则阻塞等待队列；ctx 取消返回 nil。
func (d *ReconcileDispatcher) nextDispatchInstance(ctx context.Context) *entity.GameInstance {
	d.overflowMu.Lock()
	if n := len(d.overflow); n > 0 {
		inst := d.overflow[0]
		d.overflow = d.overflow[1:]
		d.overflowMu.Unlock()
		return inst
	}
	d.overflowMu.Unlock()
	select {
	case inst := <-d.queue:
		return inst
	case <-ctx.Done():
		return nil
	}
}

func (d *ReconcileDispatcher) Dispatch(ctx context.Context, instance *entity.GameInstance) error {
	switch instance.Status {
	case entity.StatusPending:
		instance.Status = entity.StatusScheduling
		d.instanceRepo.UpdateStatus(ctx, instance)
		d.RequestDispatch(ctx, instance)
	case entity.StatusQueued:
		// 排队实例被唤醒（QueueWaker）：回到调度阶段（§2.1 状态机）
		instance.Status = entity.StatusScheduling
		d.instanceRepo.UpdateStatus(ctx, instance)
		d.RequestDispatch(ctx, instance)
	case entity.StatusScheduling:
		result, err := d.scheduler.Schedule(ctx, instance)
		if err != nil {
			slog.Error("[Scheduler] 调度器内部错误", "instanceId", instance.ID, "err", err)
			d.FailedInstance(ctx, instance)
			return nil
		}
		switch result.Outcome {
		case OutcomeScheduled:
			// 预留事务内已完成：预留扣减 + 端口映射 + node_agent_id/status 落库（§7.1）。
			// 若由排队唤醒调度成功 → 清队列行。
			if _, err := d.queueManager.Get(ctx, instance.ID); err == nil {
				_ = d.queueManager.Cancel(ctx, instance.ID)
			}
			// 同步内存对象并保存 ResourceReq（供释放预留，7.2）。
			instance.NodeAgentID = &result.NodeAgentID
			instance.Status = entity.StatusPreparingBuild
			instance.ResourceReq = &result.ResourceReq
			instance.QueuedReason = ""
			instance.QueuedAt = nil
			// 用 Save 全字段持久化（UpdateStatus 只更新 status，会丢 node_agent_id）
			d.instanceRepo.Save(ctx, instance)
			d.RequestDispatch(ctx, instance)
		case OutcomeQueued:
			// 可恢复原因（资源/端口/压力不足）→ 排队（R8）。
			// 首次入队 vs 重试仍不足（退避/超时）：
			if _, err := d.queueManager.Get(ctx, instance.ID); err != nil {
				// 首次入队
				if err := d.queueManager.Enqueue(ctx, instance, result.Reason); err != nil {
					slog.Error("[Scheduler] 排队写入失败", "instanceId", instance.ID, "err", err)
					d.FailedInstance(ctx, instance)
					return nil
				}
				now := time.Now()
				instance.Status = entity.StatusQueued
				instance.QueuedReason = result.Reason
				instance.QueuedAt = &now
				d.instanceRepo.Save(ctx, instance)
				slog.Info("[Scheduler] 实例已排队",
					"instanceId", instance.ID, "reason", result.Reason)
			} else {
				// 重试仍不足：退避 or 超时（§8.2/D9）
				timeout, err := d.queueManager.OnStillQueued(ctx, instance.ID, false)
				if err != nil {
					slog.Error("[Scheduler] 队列退避更新失败", "instanceId", instance.ID, "err", err)
					d.FailedInstance(ctx, instance)
					return nil
				}
				if timeout {
					_ = d.queueManager.Cancel(ctx, instance.ID)
					instance.Status = entity.Failed
					instance.QueuedReason = "queue_timeout"
					instance.FailReason = "排队超时（30 分钟），未获调度"
					d.instanceRepo.Save(ctx, instance)
					if d.eventBus != nil {
						d.eventBus.Publish(SchedulerEvent{Type: EventInstanceQueueTimeout, OccurredAt: time.Now(),
							InstanceID: instance.ID, Detail: "排队超时（30 分钟）"})
					}
					slog.Warn("[Scheduler] 排队超时，实例调度失败",
						"instanceId", instance.ID, "reason", result.Reason)
					return nil
				}
				instance.Status = entity.StatusQueued
				instance.QueuedReason = result.Reason
				d.instanceRepo.UpdateStatus(ctx, instance)
				slog.Info("[Scheduler] 实例仍资源不足，退避重试",
					"instanceId", instance.ID, "reason", result.Reason)
			}
		default: // OutcomeFailed
			slog.Warn("[Scheduler] 调度失败",
				"instanceId", instance.ID, "code", result.ReasonCode, "reason", result.Reason)
			instance.FailReason = result.Reason
			d.FailedInstance(ctx, instance)
		}
	case entity.StatusPreparingBuild:
		if instance.NodeAgentID == nil {
			slog.Error("[NodeAgent] NodeAgentID 为空", "instanceId", instance.ID, "status", instance.Status)
			d.FailedInstance(ctx, instance)
			return nil
		}
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
		callCtx, cancel := d.withRPCTimeout(ctx)
		resp, err := client.PrepareGameBuild(callCtx, req)
		cancel()
		if err != nil {
			d.handleDispatchError(ctx, instance, err, "PrepareGameBuild")
			return nil
		}
		go d.PollingResult(ctx, resp.Operation.OperationId, instance, client, d.onPrepareBuildSucceeded)
	case entity.StatusRestoringSnapshot:
		if instance.NodeAgentID == nil {
			slog.Error("[NodeAgent] NodeAgentID 为空", "instanceId", instance.ID, "status", instance.Status)
			d.FailedInstance(ctx, instance)
			return nil
		}
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
		acallCtx, acancel := d.withRPCTimeout(ctx)
		snapshotResp, err := d.assetClient.GetLatestSnapshot(acallCtx, &assetservicev1.GetLatestSnapshotRequest{
			InstanceId: instance.ID,
		})
		acancel()
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
		rcallCtx, rcancel := d.withRPCTimeout(ctx)
		restoreResp, err := client.RestoreSnapshot(rcallCtx, &nodeagentv1.RestoreSnapshotRequest{
			InstanceId: instance.ID,
			SnapshotId: snapshotResp.Snapshot.SnapshotId,
		})
		rcancel()
		if err != nil {
			d.handleDispatchError(ctx, instance, err, "RestoreSnapshot")
			return nil
		}
		go d.PollingResult(ctx, restoreResp.Operation.OperationId, instance, client, d.onRestoreSnapshotSucceeded)
	case entity.StatusStarting:
		if instance.NodeAgentID == nil {
			slog.Error("[NodeAgent] NodeAgentID 为空", "instanceId", instance.ID, "status", instance.Status)
			d.FailedInstance(ctx, instance)
			return nil
		}
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
		bcallCtx, bcancel := d.withRPCTimeout(ctx)
		buildResp, err := d.assetClient.GetGameBuild(bcallCtx, &assetservicev1.GetGameBuildRequest{
			BuildId: instance.GameBuildId,
		})
		bcancel()
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
		// 查询调度阶段为该实例分配的端口映射
		portMappings, err := d.gameContainerPortMapper.GetMapPortByInstanceId(ctx, instance.ID)
		if err != nil {
			slog.Error("[PortMapper] GetMapPortByInstanceId fail",
				"instanceId", instance.ID, "err", err)
			d.FailedInstance(ctx, instance)
			return nil
		}
		// 平台运营方配置合并（M5）：platform 为底、player（实例配置）覆盖，
		// 每次启动取最新 platform 配置（共享语义）
		runtimeSpec := buildInstanceRuntimeSpec(instance, buildResp.Build, containerConfig, portMappings)
		if merged := d.mergedInstanceConfig(ctx, instance); merged != nil {
			specInstance := *instance
			specInstance.Config = merged
			runtimeSpec = buildInstanceRuntimeSpec(&specInstance, buildResp.Build, containerConfig, portMappings)
		}
		// M8：外部受限凭证分配（按 build 的 adapter_metadata.credentials 声明）。
		// 晚分配：调度/构建/快照阶段失败不占用稀缺凭证；池空（required）→ 启动失败。
		if creds, err := d.allocateCredentials(ctx, instance, buildResp.Build); err != nil {
			d.failWithReason(ctx, instance, err.Error())
			return nil
		} else if len(creds) > 0 {
			runtimeSpec.Credentials = creds
		}
		scallCtx, scancel := d.withRPCTimeout(ctx)
		startResp, err := client.StartInstance(scallCtx, &nodeagentv1.StartInstanceRequest{Instance: runtimeSpec})
		scancel()
		if err != nil {
			d.handleDispatchError(ctx, instance, err, "StartInstance")
			return nil
		}
		go d.PollingResult(ctx, startResp.Operation.OperationId, instance, client, d.onStartInstanceSucceeded)
	case entity.StatusStopping:
		if instance.NodeAgentID == nil {
			slog.Error("[NodeAgent] NodeAgentID 为空", "instanceId", instance.ID, "status", instance.Status)
			d.FailedInstance(ctx, instance)
			return nil
		}
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
		tcallCtx, tcancel := d.withRPCTimeout(ctx)
		stopResp, err := client.StopInstance(tcallCtx, &nodeagentv1.StopInstanceRequest{
			InstanceId: instance.ID,
		})
		tcancel()
		if err != nil {
			d.handleDispatchError(ctx, instance, err, "StopInstance")
			return nil
		}
		go d.PollingResult(ctx, stopResp.Operation.OperationId, instance, client, d.onStopInstanceSucceeded)
	case entity.StatusCleaning:
		if instance.NodeAgentID == nil {
			slog.Error("[NodeAgent] NodeAgentID 为空", "instanceId", instance.ID, "status", instance.Status)
			d.FailedInstance(ctx, instance)
			return nil
		}
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
		ccallCtx, ccancel := d.withRPCTimeout(ctx)
		cleanResp, err := client.CleanInstance(ccallCtx, &nodeagentv1.CleanInstanceRequest{
			InstanceId: instance.ID,
		})
		ccancel()
		if err != nil {
			d.handleDispatchError(ctx, instance, err, "CleanInstance")
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
	// M8：失败即释放凭证占用（幂等：未分配则无操作），避免凭证被失败实例长期占用
	if d.credentialUC != nil {
		if err := d.credentialUC.ReleaseByInstance(ctx, instance.ID); err != nil {
			slog.Error("[ReconcileDispatcher] 释放凭证失败",
				"instanceId", instance.ID, "err", err)
		}
	}
	// 释放预留（7.2）：仅当实例已绑定节点且失败发生在"调度成功之后"（阶段失败/运行中失败）。
	// 调度阶段失败（Scheduling→Failed）本次从未成功绑定/扣减——不应释放任何预留，
	// 否则会误释放实例残留的上次预留（NodeAgentID/ResourceReq 未清时），
	// 表现为"实例调度失败，但节点预留发生了变化"。
	if instance.NodeAgentID != nil && instance.ResourceReq != nil &&
		instance.Status != entity.StatusScheduling {
		d.releaseReservation(ctx, instance)
	}
	// 停止/清理阶段的失败保留 node_agent 绑定：StopInstance 可能已失败而容器仍残留在
	// node_agent 上，此时清空绑定会导致"停止失败后重试停止"在 StatusStopping 分支因
	// NodeAgentID 为空而直接再次失败（无法定位节点清理残留容器）。
	// 绑定保留的同时清空 ResourceReq（预留已释放），避免重试停止成功进入清理阶段
	// onCleanInstanceSucceeded 时二次释放预留。
	keepBinding := instance.Status == entity.StatusStopping || instance.Status == entity.StatusCleaning
	instance.Status = entity.Failed
	if keepBinding {
		instance.ResourceReq = nil
	} else {
		instance.NodeAgentID = nil // 失败后不再绑定节点（预留已释放或从未扣减）
	}
	// Save 全字段落库（含 fail_reason、NodeAgentID 清理）
	d.instanceRepo.Save(ctx, instance)
	if d.eventBus != nil {
		d.eventBus.Publish(SchedulerEvent{Type: EventInstanceFailed, OccurredAt: time.Now(),
			InstanceID: instance.ID,
			NodeAgentID: derefStr(instance.NodeAgentID),
			Detail:     instance.FailReason})
	}
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// releaseReservation 扣回预留（幂等：仅当已绑定节点且记录了资源需求时执行）
func (d *ReconcileDispatcher) releaseReservation(ctx context.Context, instance *entity.GameInstance) {
	if instance.NodeAgentID == nil || instance.ResourceReq == nil {
		return
	}
	if agent, err := d.nodeAgnetRepo.GetByID(ctx, *instance.NodeAgentID); err == nil {
		if err := d.reservationRepo.Release(ctx, agent.NodeId, *instance.ResourceReq); err != nil {
			slog.Error("[ReconcileDispatcher] 释放预留失败",
				"instanceId", instance.ID, "nodeAgentId", *instance.NodeAgentID, "err", err)
		} else {
			slog.Info("[ReconcileDispatcher] 释放预留",
				"instanceId", instance.ID, "nodeAgentId", *instance.NodeAgentID,
				"nodeId", agent.NodeId, "cpuMilli", instance.ResourceReq.CPUMilli,
				"memBytes", instance.ResourceReq.MemoryBytes)
		}
	}
}

// SetResourceReleasedHook 注册资源释放事件回调（S14：实例停止释放资源后唤醒排队）
func (d *ReconcileDispatcher) SetResourceReleasedHook(fn func()) {
	d.resourceReleasedHook = fn
}

/**
* 从 gRPC 错误中解析业务错误详情(nodeagent.v1.ErrorDetail,rich error model)
**/
func extractErrorDetail(err error) *nodeagentv1.ErrorDetail {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	for _, d := range st.Details() {
		if ed, ok := d.(*nodeagentv1.ErrorDetail); ok {
			return ed
		}
	}
	return nil
}

/**
* B-12（P0）：为单条 node_agent/asset_service RPC 派生带超时的调用 ctx。
* reconcile 消费端是单 goroutine，无超时的挂起 RPC 会冻结整条管线；
* 所有同步 RPC（PrepareGameBuild/RestoreSnapshot/Start/Stop/Clean/GetOperation/Inspect/asset 查询）
* 必须经由本方法包裹。
**/
func (d *ReconcileDispatcher) withRPCTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if d.rpcTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d.rpcTimeout)
}

/**
* B-12（P0）：RPC 瞬态错误判定（网络层/超时类，可重试）：
* - context.DeadlineExceeded（调用方超时）
* - gRPC Unavailable（对端不可达）/ DeadlineExceeded / Aborted（服务端重入冲突）
* 与 node_agent rich error 的 Retryable 标志互补（后者只覆盖业务层可重试）。
**/
func isRetryableRPCErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
		return true
	}
	return false
}

/**
* 自动重试计数:返回 false 表示已达重试上限,不应再重试。
* 计数按实例累积,成功时由 clearRetryCount 清零。
**/
func (d *ReconcileDispatcher) retryOperation(ctx context.Context, instance *entity.GameInstance) bool {
	d.retryMu.Lock()
	defer d.retryMu.Unlock()
	count := d.operationRetryCounts[instance.ID] + 1
	if count > OPERATION_RETRY_MAX {
		delete(d.operationRetryCounts, instance.ID)
		return false
	}
	d.operationRetryCounts[instance.ID] = count
	slog.Warn("[ReconcileDispatcher] 操作失败,触发自动重试",
		"instanceId", instance.ID, "retry", count, "max", OPERATION_RETRY_MAX)
	return true
}

func (d *ReconcileDispatcher) clearRetryCount(instanceID string) {
	d.retryMu.Lock()
	defer d.retryMu.Unlock()
	delete(d.operationRetryCounts, instanceID)
}

/**
* 统一处理派发阶段的同步 RPC 失败:记录结构化错误;可重试错误重新入队,否则标记失败。
* opName 用于日志标识(如 "PrepareGameBuild")。
* 可重试 = node_agent rich error 的 Retryable 标志，或 B-12 网络层瞬态（超时/Unavailable）。
**/
func (d *ReconcileDispatcher) handleDispatchError(ctx context.Context, instance *entity.GameInstance, err error, opName string) {
	detail := extractErrorDetail(err)
	retryable := isRetryableRPCErr(err)
	if detail != nil {
		slog.Error("[NodeAgentClients] "+opName+" fail",
			"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
			"ErrorCode", detail.GetCode().String(), "Category", detail.GetCategory().String(),
			"Retryable", detail.GetRetryable(), "Params", detail.GetParams(),
			"Message", detail.GetMessage(),
		)
		retryable = retryable || detail.GetRetryable()
		if retryable && d.retryOperation(ctx, instance) {
			slog.Warn("[NodeAgentClients] "+opName+" 失败可重试,重新入队调度",
				"instanceId", instance.ID, "Status", instance.Status,
			)
			if err := d.RequestDispatch(ctx, instance); err == nil {
				return
			}
		}
	} else {
		slog.Error("[NodeAgentClients] "+opName+" fail",
			"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID, "err", err,
		)
		if retryable && d.retryOperation(ctx, instance) {
			slog.Warn("[NodeAgentClients] "+opName+" 网络层瞬态失败,重新入队调度",
				"instanceId", instance.ID, "Status", instance.Status,
			)
			if err := d.RequestDispatch(ctx, instance); err == nil {
				return
			}
		}
	}
	instance.FailReason = opName + " 失败: " + err.Error()
	d.FailedInstance(ctx, instance)
}

/**
* M8：按 build 的 adapter_metadata.credentials 声明分配外部受限凭证。
* 返回 map[注入key]secret（node_agent 写入 /data/.platform/{key}）。
* required 且池空 → 返回错误（调用方标记实例失败）。
**/
func (d *ReconcileDispatcher) allocateCredentials(ctx context.Context, instance *entity.GameInstance, build *assetservicev1.GameBuild) (map[string]string, error) {
	if d.credentialUC == nil || build.GetAdapterMetadata() == nil {
		return nil, nil
	}
	specs := build.GetAdapterMetadata().GetCredentials()
	if len(specs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(specs))
	for _, spec := range specs {
		secret, err := d.credentialUC.AllocateForInstance(ctx, instance.GameID, spec.GetPool(), instance.ID)
		if err != nil {
			if spec.GetRequired() {
				return nil, fmt.Errorf("allocate credential %s (pool=%s): %w", spec.GetKey(), spec.GetPool(), err)
			}
			// 非必需凭证池空：跳过注入，不影响启动
			slog.Warn("[ReconcileDispatcher] 非必需凭证池空，跳过注入",
				"instanceId", instance.ID, "pool", spec.GetPool())
			continue
		}
		out[spec.GetKey()] = secret
		slog.Info("[ReconcileDispatcher] 凭证已分配",
			"instanceId", instance.ID, "key", spec.GetKey(), "pool", spec.GetPool())
	}
	return out, nil
}

// failWithReason 设置失败原因并标记实例失败（同时幂等释放凭证占用）
func (d *ReconcileDispatcher) failWithReason(ctx context.Context, instance *entity.GameInstance, reason string) {
	instance.FailReason = reason
	d.FailedInstance(ctx, instance)
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
		callCtx, cancel := d.withRPCTimeout(ctx)
		resp, err := client.GetOperation(callCtx, req)
		cancel()
		if err != nil {
			// B-12：网络层瞬态（超时/Unavailable）→ 继续轮询（操作可能仍在 node_agent 上推进），
			// 不误杀；非瞬态错误才标记失败。
			if isRetryableRPCErr(err) {
				slog.Warn("[NodeAgentClients] GetOperation 瞬态错误,继续轮询",
					"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
					"OperationId", operation_id, "err", err,
				)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			slog.Error("[NodeAgentClients] GetOperation fail",
				"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
				"OperationId", operation_id, "err", err,
			)
			d.FailedInstance(ctx, instance)
			return
		}
		if resp.Operation.Status == nodeagentv1.OperationStatus_OPERATION_STATUS_FAILED {
			detail := resp.Operation.GetError()
			if detail != nil {
				slog.Error("[NodeAgentClients] GetOperation fail",
					"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
					"OperationId", operation_id,
					"ErrorCode", detail.GetCode().String(), "Category", detail.GetCategory().String(),
					"Retryable", detail.GetRetryable(), "Params", detail.GetParams(),
					"Message", detail.GetMessage(),
				)
				// 可重试错误:实例状态保持可调度,重新入队执行本阶段
				if detail.GetRetryable() && d.retryOperation(ctx, instance) {
					slog.Warn("[NodeAgentClients] GetOperation 失败可重试,重新入队调度",
						"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
						"OperationId", operation_id, "Status", instance.Status,
					)
					if err := d.RequestDispatch(ctx, instance); err == nil {
						return
					}
				}
			} else {
				slog.Error("[NodeAgentClients] GetOperation fail",
					"NodeAgentID", instance.NodeAgentID, "instanceId", instance.ID,
					"OperationId", operation_id,
					"Message", resp.Operation.Message,
				)
			}
			d.FailedInstance(ctx, instance)
			return
		}
		if resp.Operation.Status == nodeagentv1.OperationStatus_OPERATION_STATUS_SUCCEEDED {
			d.clearRetryCount(instance.ID)
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
* 7.2 挂点：释放端口映射 + 预留；并触发资源释放事件（S14 排队唤醒）。
**/
func (d *ReconcileDispatcher) onCleanInstanceSucceeded(ctx context.Context, instance *entity.GameInstance) {
	// M8：实例彻底停止 → 释放凭证占用（幂等），归还池复用
	if d.credentialUC != nil {
		if err := d.credentialUC.ReleaseByInstance(ctx, instance.ID); err != nil {
			slog.Error("[ReconcileDispatcher] 停止释放凭证失败",
				"instanceId", instance.ID, "err", err)
		}
	}
	// 先释放预留（依赖 NodeAgentID），再清空绑定
	d.releaseReservation(ctx, instance)
	instance.Status = entity.StatusStopped
	instance.NodeAgentID = nil
	// Save 全字段持久化（UpdateStatus 只更新 status 列，会丢 node_agent_id 清空）
	d.instanceRepo.Save(ctx, instance)
	if _, err := d.gameContainerPortMapper.ReleaseMapPortByInstanceId(ctx, instance.ID); err != nil {
		slog.Error("[PortMapper] ReleaseMapPortByInstanceId fail",
			"instanceId", instance.ID, "err", err)
	}
	// 资源释放 → 唤醒排队（S14）
	if d.resourceReleasedHook != nil {
		d.resourceReleasedHook()
	}
	if d.eventBus != nil {
		d.eventBus.Publish(SchedulerEvent{Type: EventInstanceStopped, OccurredAt: time.Now(),
			InstanceID: instance.ID, Detail: "实例停止（释放预留与端口）"})
	}
}

// mergedInstanceConfig 合并平台配置与实例配置（M5）：platform 为底、player 覆盖。
// 平台配置不存在或为空时返回 nil（保持原 instance.Config 语义，避免不必要复制）。
func (d *ReconcileDispatcher) mergedInstanceConfig(ctx context.Context, instance *entity.GameInstance) map[string]string {
	if d.platformConfigRepo == nil {
		return nil
	}
	pc, err := d.platformConfigRepo.GetByGame(ctx, instance.GameID)
	if err != nil || pc == nil || len(pc.Config) == 0 {
		return nil
	}
	if len(instance.Config) == 0 {
		return pc.Config
	}
	merged := make(map[string]string, len(pc.Config)+len(instance.Config))
	for k, v := range pc.Config {
		merged[k] = v
	}
	for k, v := range instance.Config {
		merged[k] = v
	}
	return merged
}

/**
* 构造 start_instance 所需的 InstanceRuntimeSpec
**/
func buildInstanceRuntimeSpec(
	instance *entity.GameInstance,
	build *assetservicev1.GameBuild,
	config *entity.GameContainerConfig,
	portMappings []entity.ContainerPortMapping,
) *nodeagentv1.InstanceRuntimeSpec {
	return &nodeagentv1.InstanceRuntimeSpec{
		InstanceId:          instance.ID,
		Build:               mapGameBuild(build),
		ContainerServerPath: config.ContainerServerPath,
		PortMapping:         mapPortMapping(config, portMappings),
		// 端口注入：把游戏端口对应的宿主端口通过 env 传给 adapter
		// （adapter start.sh 用它改写游戏配置，使游戏通告端口 == 宿主端口）
		Env: buildInstanceEnv(config, portMappings),
		// 实例配置（000024，M3）：platform + player 合并键值，
		// node_agent 写入 /data/.platform/game-config.json 供容器内 config-render 渲染
		Config: instance.Config,
		// spec 目前无数据来源，先填占位结构体以满足 nodeagent 的校验
		Spec: &nodeagentv1.InstanceSpec{
			Resources: &nodeagentv1.ResourceRequirements{},
		},
	}
}

// defaultPortInjectEnv 端口注入 env 默认变量名（000024，M3）：
// 各游戏 adapter 的 port_inject.env 若未在 game_container_configs 覆盖，统一用此名
const defaultPortInjectEnv = "GAME_HOST_PORT"

/**
* buildInstanceEnv 构造容器环境变量：
* 注入模式（InjectGamePort）下，向 adapter 传递 <port_inject_env>=<游戏端口宿主端口>。
* env 变量名读自 game_container_configs.port_inject_env（默认 GAME_HOST_PORT），
* 消灭 SDTD_SERVER_PORT 类平台硬编码；适配器在 hooks.sh 中消费该变量改写游戏配置。
* 未启用注入时返回 nil（空 env，nodeagent 不注入任何环境变量）。
**/
func buildInstanceEnv(config *entity.GameContainerConfig, portMappings []entity.ContainerPortMapping) map[string]string {
	if config == nil || !config.InjectGamePort {
		return nil
	}
	envName := config.PortInjectEnv
	if envName == "" {
		envName = defaultPortInjectEnv
	}
	for _, m := range portMappings {
		if m.IsGamePort {
			return map[string]string{
				envName: strconv.Itoa(int(m.HostPort)),
			}
		}
	}
	return nil
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
* 将实例已分配的端口映射映射为 nodeagent 的 PortMapping
**/
func mapPortMapping(config *entity.GameContainerConfig, mappings []entity.ContainerPortMapping) *nodeagentv1.PortMapping {
	pm := &nodeagentv1.PortMapping{}
	switch config.PortMode {
	case entity.PORT_MAPPING_MOD_HOST:
		pm.Mode = nodeagentv1.PortMappingMod_PORT_MAPPING_MOD_HOST
	default:
		pm.Mode = nodeagentv1.PortMappingMod_PORT_MAPPING_MOD_NAT
	}
	for _, m := range mappings {
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
			if err := d.NextDispatch(ctx); err != nil {
				slog.Info("ReconcileDispatcher 退出", "err", err)
				return
			}
		}
	}()
}

/**
* 恢复调度：程序重启后，把处于中间态（未完成生命周期）的实例重新加入调度队列
**/
func (d *ReconcileDispatcher) Recover(ctx context.Context) error {
	instances, err := d.instanceRepo.ListByStatuses(ctx, dispatchableStatuses...)
	if err != nil {
		return fmt.Errorf("recover: list dispatchable instances: %w", err)
	}
	for _, inst := range instances {
		if err := d.RequestDispatch(ctx, inst); err != nil {
			slog.Warn("[Recover] 实例入队失败", "instanceId", inst.ID, "status", inst.Status, "err", err)
		}
	}
	slog.Info("[Recover] 恢复待调度实例", "count", len(instances))
	return nil
}

/**
* WatchRunningInstances 运行实例健康巡检（启动失败/运行崩溃用户可见性闭环）：
* node_agent 的 BackendContainerChecker 每秒检测容器状态（Exited → 本地实例 Failed，
* 并采集容器日志尾部作为 fail_reason），但不会主动上报 controller —— 本方法周期拉取
* InspectInstance 补齐断链：发现 node_agent 侧实例已 Failed → controller 标记失败并
* 透传原因（前端展示 fail_reason），同时走 FailedInstance 完整失败路径（释放凭证/预留）。
* B-14（P0）：节点失联 fencing —— Inspect 不可达且健康监控判失联超阈值 → 置 Failed，
* 释放订阅槽位/凭证/预留，避免失联节点的运行实例永久占用。
* 由 main 启动的 ticker goroutine 周期调用。
**/
func (d *ReconcileDispatcher) WatchRunningInstances(ctx context.Context) {
	instances, err := d.instanceRepo.ListByStatuses(ctx, entity.StatusRunning)
	if err != nil {
		slog.Error("[WatchRunningInstances] 查询 running 实例失败", "err", err)
		return
	}
	for _, instance := range instances {
		if instance.NodeAgentID == nil {
			continue
		}
		nodeAgent, err := d.nodeAgnetRepo.GetByID(ctx, *instance.NodeAgentID)
		if err != nil || nodeAgent == nil {
			continue
		}
		node, err := d.nodeRepo.GetByID(nodeAgent.NodeId)
		if err != nil || node == nil {
			continue
		}
		client, err := d.nodeAgentClients.Get(ctx, *instance.NodeAgentID, fmt.Sprintf("%s:%d", node.Ip, nodeAgent.Port))
		if err != nil {
			continue
		}
		callCtx, cancel := d.withRPCTimeout(ctx)
		resp, err := client.InspectInstance(callCtx, &nodeagentv1.InspectInstanceRequest{InstanceId: instance.ID})
		cancel()
		if err != nil {
			// B-14（P0）：节点不可达 → fencing。
			// 健康监控已判失联（Alive=false / unhealthy）且 Inspect 持续失败超过阈值 →
			// 置 Failed（释放槽位/凭证/预留，走 FailedInstance 完整失败路径）；
			// 健康未判失联的瞬态（心跳正常但本 RPC 抖动）不累计，避免误杀。
			if !nodeAgent.Alive || nodeAgent.HealthStatus == entity.HealthUnhealthy {
				if d.markOfflineAndShouldFence(instance.ID) {
					d.clearOffline(instance.ID)
					slog.Warn("[WatchRunningInstances] 节点失联超阈值，运行实例置失败（fencing）",
						"instanceId", instance.ID, "nodeAgentId", *instance.NodeAgentID,
						"threshold", d.fencingTimeout.String())
					d.failWithReason(ctx, instance,
						"节点失联超过 "+d.fencingTimeout.String()+"，实例已终止（fencing）；节点恢复后请重试")
				}
			} else {
				d.clearOffline(instance.ID) // 瞬态不可达：不累计
			}
			continue
		}
		d.clearOffline(instance.ID)
		naInstance := resp.GetInstance()
		if naInstance == nil {
			continue
		}
		if naInstance.GetStatus() != nodeagentv1.NodeAgentGameInstanceStatus_FAILED {
			continue
		}
		reason := naInstance.GetFailReason()
		if reason == "" {
			reason = "游戏进程退出（node_agent 检测到容器异常）"
		}
		slog.Warn("[WatchRunningInstances] 检测到运行实例实际已失败",
			"instanceId", instance.ID, "reason", reason)
		d.failWithReason(ctx, instance, reason)
	}
}

// markOfflineAndShouldFence 记录实例首次"节点不可达"时间；累计超过 fencingTimeout 返回 true。
// 幂等：重复调用只记账，不重置计时（保证"持续失联"而非"多次瞬时抖动"才触发）。
func (d *ReconcileDispatcher) markOfflineAndShouldFence(instanceID string) bool {
	d.offlineMu.Lock()
	defer d.offlineMu.Unlock()
	first, ok := d.offlineSince[instanceID]
	now := time.Now()
	if !ok {
		d.offlineSince[instanceID] = now
		return false
	}
	return now.Sub(first) >= d.fencingTimeout
}

// clearOffline 清除实例的失联计时（节点恢复可达/实例已置失败时调用）。
func (d *ReconcileDispatcher) clearOffline(instanceID string) {
	d.offlineMu.Lock()
	defer d.offlineMu.Unlock()
	delete(d.offlineSince, instanceID)
}