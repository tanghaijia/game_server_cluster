package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"controller-go/config"
	"controller-go/internal/biz"
	"controller-go/internal/client/assetservice"
	nodeagentclient "controller-go/internal/client/nodeagent"
	"controller-go/internal/handler"
	repogorm "controller-go/internal/repository/gorm"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// ---------------------------------------------------------------
	// 1. 配置
	// ---------------------------------------------------------------
	cfg := config.Load()
	slog.Info("配置加载完成", "db_host", cfg.DBHost, "db_port", cfg.DBPort)

	// ---------------------------------------------------------------
	// 2. 数据库
	// ---------------------------------------------------------------
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
	})
	if err != nil {
		slog.Error("数据库连接失败", "err", err)
		os.Exit(1)
	}
	slog.Info("数据库连接成功")

	// 版本化迁移
	if err := runMigrations(db); err != nil {
		slog.Error("数据库迁移失败", "err", err)
		os.Exit(1)
	}
	slog.Info("数据库迁移完成")

	// ---------------------------------------------------------------
	// 3. Repository
	// ---------------------------------------------------------------
	gameRepo := repogorm.NewGameRepo(db)
	gameInstanceRepo := repogorm.NewGameInstanceRepo(db)
	nodeAgentRepo := repogorm.NewNodeAgentRepo(db)
	nodeRepo := repogorm.NewNodeRepo(db)
	gameContainerConfigRepo := repogorm.NewGameContainerConfigRepo(db)
	containerPortMappingRepo := repogorm.NewContainerPortMappingRepo(db)
	steamBranchRepo := repogorm.NewSteamBranchRepo(db)
	sampleRepo := repogorm.NewNodeResourceSampleRepo(db)
	reservationRepo := repogorm.NewReservationRepo(db)
	queueRepo := repogorm.NewSchedulingQueueRepo(db)

	// ---------------------------------------------------------------
	// 4. gRPC 客户端
	// ---------------------------------------------------------------
	// AssetService (长连接，全局一个)
	assetConn, err := grpc.NewClient(cfg.AssetServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		slog.Error("连接 AssetService 失败", "addr", cfg.AssetServiceAddr, "err", err)
		os.Exit(1)
	}
	defer assetConn.Close()
	assetClient := assetservice.NewAssetServiceFaceClient(assetConn)
	businessClient := assetservice.NewBusinessServiceFaceClient(assetConn)
	slog.Info("AssetService 客户端创建成功", "addr", cfg.AssetServiceAddr)

	// NodeAgent (按需懒加载，通过 ClientRegistry 管理)
	nodeAgentClients := nodeagentclient.NewClientRegistry()
	defer nodeAgentClients.CloseAll()

	// ---------------------------------------------------------------
	// 5. 调度器（ResourceAwareScheduler：filter → score → 预留事务）
	//    依赖 GameCacheManager（H5 缓存判定）与 GameContainerPortMapper（H4 端口预检）
	// ---------------------------------------------------------------
	gameContainerPortMapper := biz.NewGameContainerPortMapper(containerPortMappingRepo, gameContainerConfigRepo)
	gameCacheManager := biz.NewGameCacheManager(nodeAgentClients, assetClient, businessClient, steamBranchRepo, nodeAgentRepo, nodeRepo, gameRepo)
	// 调度事件总线（S30 观测）：调度器/队列/压力/健康/缓存发布事件；双写内存 + DB 持久化（重启可回溯）
	eventRepo := repogorm.NewSchedulerEventRepo(db)
	eventBus := biz.NewSchedulerEventBus(cfg.EventBufferSize, eventRepo)
	// 调度统计持久化（S29 指标）：scheduled/queued/failed 计数落库，重启不归零
	statRepo := repogorm.NewSchedulerStatRepo(db)
	// game-cache 视图（§10）：快照供 H5 判定，周期刷新（替代调度时实时 gRPC 查询）
	nodeCacheView := biz.NewNodeCacheView(gameCacheManager, nodeAgentRepo, steamBranchRepo, gameRepo, eventBus)

	schedulerWeights := biz.DefaultScoreWeights()
	if cfg.SchedulerScoreWeights != "" {
		if err := json.Unmarshal([]byte(cfg.SchedulerScoreWeights), &schedulerWeights); err != nil {
			slog.Error("解析调度评分权重失败，使用默认权重", "err", err)
		}
	}
	queueManager := biz.NewQueueManager(
		queueRepo,
		time.Duration(cfg.QueueBackoffBaseSec)*time.Second,
		time.Duration(cfg.QueueBackoffMaxSec)*time.Second,
		time.Duration(cfg.QueueTimeoutMin)*time.Minute,
	)
	scheduler := biz.NewResourceAwareScheduler(
		nodeAgentRepo,
		nodeRepo,
		gameInstanceRepo,
		sampleRepo,
		reservationRepo,
		gameRepo,
		gameContainerConfigRepo,
		steamBranchRepo,
		gameContainerPortMapper,
		nodeCacheView,
		queueManager,
		eventBus,
		statRepo,
		schedulerWeights,
		cfg.SchedulerUtilizationTarget,
		cfg.SchedulerRegionForce,
		cfg.SchedulerReservationRetry,
		time.Duration(cfg.SchedulerHistoryWindowSec)*time.Second,
		time.Duration(cfg.SchedulerHealthStaleSec)*time.Second,
		cfg.SchedulerCacheSpillWatermark,
		cfg.SchedulerCacheUpdateBufferRatio,
	)
	slog.Info("Scheduler 就绪", "weights", schedulerWeights, "utilization_target", cfg.SchedulerUtilizationTarget)

	// ---------------------------------------------------------------
	// 6. ReconcileDispatcher
	// ---------------------------------------------------------------
	platformConfigRepo := repogorm.NewGamePlatformConfigRepo(db)
	// M8：外部受限凭证池（如 DST cluster_token）
	credentialPoolRepo := repogorm.NewCredentialPoolRepo(db)
	credentialUC := biz.NewCredentialUseCase(credentialPoolRepo, assetClient)
	dispatcher := biz.NewReconcileDispatcher(
		gameInstanceRepo,
		nodeAgentRepo,
		nodeRepo,
		scheduler,
		reservationRepo,
		queueManager,
		eventBus,
		nodeAgentClients,
		assetClient,
		gameRepo,
		gameContainerConfigRepo,
		*gameContainerPortMapper,
		platformConfigRepo,
		credentialUC,
		time.Duration(cfg.RPCTimeoutSec)*time.Second,   // B-12：单条 RPC 超时
		time.Duration(cfg.NodeOfflineFenceMin)*time.Minute, // B-14：节点失联 fencing 阈值
	)

	// 排队唤醒器（P2，§8.3）
	queueWaker := biz.NewQueueWaker(
		queueRepo, gameInstanceRepo, dispatcher, cfg.QueueMaxWakePerRound,
	)
	// 实例停止释放资源 → 事件唤醒排队（S14）
	dispatcher.SetResourceReleasedHook(queueWaker.Wake)
	// 缓存就绪（转 AVAILABLE）→ 事件唤醒排队（S14/§10）
	nodeCacheView.SetOnCacheReady(queueWaker.Wake)

	// ---------------------------------------------------------------
	// 7. Use Cases
	// ---------------------------------------------------------------
	gameUseCase := biz.NewGameUseCase(gameRepo, steamBranchRepo, gameInstanceRepo, containerPortMappingRepo, gameContainerConfigRepo, businessClient)
	gameInstanceUseCase := biz.NewGameInstanceUseCase(gameInstanceRepo, containerPortMappingRepo, nodeAgentRepo, nodeRepo, gameRepo, gameContainerConfigRepo, dispatcher, scheduler, queueManager, assetClient)
	_ = biz.NewGameInstanceAdvanceUseCase(scheduler, gameInstanceRepo, assetClient)
	nodeUseCase := biz.NewNodeUseCase(nodeRepo, nodeAgentRepo)
	nodeAgentUseCase := biz.NewNodeAgentUseCase(nodeAgentRepo, nodeRepo)
	debugUseCase := biz.NewDebugUseCase(dispatcher, gameInstanceRepo, containerPortMappingRepo, nodeAgentRepo, nodeRepo, scheduler)
	fileSessionIssuer := biz.NewFileSessionIssuer(cfg.NodeAgentFileSecret, 30*time.Minute)
	observerUseCase := biz.NewObserverUseCase(
		scheduler, nodeRepo, nodeAgentRepo, sampleRepo, queueRepo, gameInstanceRepo,
		gameRepo, gameContainerConfigRepo, steamBranchRepo, eventBus,
		cfg.SchedulerUtilizationTarget, nodeCacheView,
	)

	// ---------------------------------------------------------------
	// 8. 启动后台组件
	// ---------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dispatcher.Start(ctx)
	slog.Info("ReconcileDispatcher 已启动")

	// 调度事件持久化消费者（批量 flush + 定期清理）
	eventBus.Start(ctx)
	slog.Info("SchedulerEventBus 已启动（事件持久化）")

	// 排队唤醒器（定时扫描 + 事件）
	queueWaker.Start(ctx, time.Duration(cfg.QueueScanIntervalSec)*time.Second)
	slog.Info("QueueWaker 已启动", "scan_interval_sec", cfg.QueueScanIntervalSec)

	// node_agent 存活检测 + 健康状态机 + 资源采样（3.4/§9）
	healthMonitor := biz.NewNodeAgentHealthMonitor(
		nodeAgentRepo, nodeRepo, sampleRepo, nodeAgentClients, scheduler, eventBus,
		time.Duration(cfg.HeartbeatProbeTimeoutMs)*time.Millisecond,
		cfg.HeartbeatFailThreshold,
		cfg.HealthDegradedPct,
	)
	healthMonitor.Start(ctx, time.Duration(cfg.HeartbeatCheckIntervalSec)*time.Second)
	slog.Info("NodeAgentHealthMonitor 已启动", "interval_sec", cfg.HeartbeatCheckIntervalSec)

	// 节点压力状态机（3.3）
	pressureMonitor := biz.NewPressureMonitor(
		nodeRepo, sampleRepo, eventBus,
		cfg.PressureWarningPct, cfg.PressureCriticalPct,
		cfg.PressureObservePeriods, cfg.PressureRecoverPeriods,
		time.Duration(cfg.SchedulerHistoryWindowSec)*time.Second,
	)
	pressureMonitor.Start(ctx, time.Duration(cfg.HeartbeatCheckIntervalSec)*time.Second)
	slog.Info("PressureMonitor 已启动")

	// B-04/P1-1：实例运行时统计缓存（node_agent 探针 → 心跳 → 本缓存 → /runtime 端点）
	runtimeStats := biz.NewRuntimeStatsRegistry()
	healthMonitor.SetRuntimeStatsRegistry(runtimeStats)

	// 中间态卡死哨兵（7.4）
	reaper := biz.NewStaleReservationReaper(
		gameInstanceRepo, nodeAgentRepo, reservationRepo, eventBus,
		time.Duration(cfg.StaleReservationTimeoutMin)*time.Minute,
	)
	reaper.Start(ctx, time.Duration(cfg.StaleReservationScanSec)*time.Second)
	slog.Info("StaleReservationReaper 已启动")

	// 启动 GameCache 后台循环（分支同步 + Enable 分支缓存下载/更新）
	gameCacheManager.Start(ctx, time.Duration(cfg.GameCacheReconcileInterval)*time.Second)

	// game-cache 快照刷新（§10）
	nodeCacheView.Start(ctx, time.Duration(cfg.CacheViewRefreshSec)*time.Second)
	slog.Info("NodeCacheView 已启动", "refresh_sec", cfg.CacheViewRefreshSec)

	// 恢复上次未完成的实例调度
	if err := dispatcher.Recover(ctx); err != nil {
		slog.Error("恢复实例调度失败", "err", err)
	}

	// 运行实例健康巡检（启动失败/运行崩溃用户可见性闭环）：
	// 周期拉取 node_agent InspectInstance，发现实际已失败则标记实例 Failed 并透传原因
	instanceWatchInterval := 15 * time.Second
	go func() {
		ticker := time.NewTicker(instanceWatchInterval)
		defer ticker.Stop()
		slog.Info("运行实例健康巡检已启动", "interval", instanceWatchInterval.String())
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dispatcher.WatchRunningInstances(ctx)
			}
		}
	}()

	// ---------------------------------------------------------------
	// 9. HTTP API (gin)
	// ---------------------------------------------------------------
	router := gin.Default()
	gameInstanceHandler := handler.NewGameInstanceHandler(gameInstanceUseCase)
	gameInstanceHandler.SetRuntimeStatsRegistry(runtimeStats) // B-04/P1-1：/runtime 端点读缓存
	gameInstanceHandler.RegisterRoutes(router)
	handler.NewGameHandler(gameUseCase, assetClient).RegisterRoutes(router)
	handler.NewGameContainerConfigHandler(biz.NewGameContainerConfigUseCase(gameRepo, gameContainerConfigRepo)).RegisterRoutes(router)
	// 平台运营方配置（M5）：/api/games/:id/platform-config
	handler.NewGamePlatformConfigHandler(biz.NewPlatformConfigUseCase(platformConfigRepo, assetClient)).RegisterRoutes(router)
	// M8：凭证池管理
	handler.NewCredentialPoolHandler(credentialUC).RegisterRoutes(router)
	handler.NewNodeHandler(nodeUseCase).RegisterRoutes(router)
	handler.NewNodeAgentHandler(nodeAgentUseCase).RegisterRoutes(router)
	handler.NewGameCacheHandler(gameCacheManager).RegisterRoutes(router)
	handler.NewFileSessionHandler(gameInstanceUseCase, nodeAgentRepo, nodeRepo, fileSessionIssuer, cfg.NodeAgentFilePortOffset).RegisterRoutes(router)
	handler.NewDebugHandler(debugUseCase).RegisterRoutes(router)
	handler.NewObserverHandler(observerUseCase).RegisterRoutes(router)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: router,
	}
	go func() {
		slog.Info("HTTP server 启动", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server 运行失败", "err", err)
		}
	}()
	slog.Info("服务启动完成", "http_port", cfg.HTTPPort)
	fmt.Printf("Controller-Go 已启动，监听 :%d\n", cfg.HTTPPort)

	// ---------------------------------------------------------------
	// 10. 优雅退出
	// ---------------------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("收到退出信号", "signal", sig)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server 关闭失败", "err", err)
	}
	slog.Info("服务已关闭")
}