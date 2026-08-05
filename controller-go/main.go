package main

import (
	"context"
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
	// 5. Scheduler（从 DB 加载 node_agent 列表用于调度）
	// ---------------------------------------------------------------
	nodeAgentIDs, err := nodeAgentRepo.ListEnabledIDs(context.Background())
	if err != nil {
		slog.Error("加载 node_agent 列表失败", "err", err)
		os.Exit(1)
	}
	scheduler := biz.NewSimpleScheduler(nodeAgentIDs)
	slog.Info("Scheduler 就绪", "node_agent_ids", nodeAgentIDs)

	// ---------------------------------------------------------------
	// 6. ReconcileDispatcher
	// ---------------------------------------------------------------
	gameContainerPortMapper := biz.NewGameContainerPortMapper(containerPortMappingRepo, gameContainerConfigRepo)
	dispatcher := biz.NewReconcileDispatcher(
		gameInstanceRepo,
		nodeAgentRepo,
		nodeRepo,
		scheduler,
		nodeAgentClients,
		assetClient,
		gameRepo,
		gameContainerConfigRepo,
		*gameContainerPortMapper,
	)

	// ---------------------------------------------------------------
	// 7. Use Cases
	// ---------------------------------------------------------------
	_ = biz.NewGameUseCase(gameRepo)
	gameInstanceUseCase := biz.NewGameInstanceUseCase(gameInstanceRepo, dispatcher)
	_ = biz.NewGameInstanceAdvanceUseCase(scheduler, gameInstanceRepo, assetClient)
	_ = biz.NewNodeUseCase(nodeRepo)
	_ = biz.NewNodeAgentUseCase(nodeAgentRepo, nodeRepo)
	_ = biz.NewGameCacheManager(nodeAgentClients, assetClient, businessClient, steamBranchRepo)

	// ---------------------------------------------------------------
	// 8. 启动 dispatcher
	// ---------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dispatcher.Start(ctx)
	slog.Info("ReconcileDispatcher 已启动")

	// 恢复上次未完成的实例调度
	if err := dispatcher.Recover(ctx); err != nil {
		slog.Error("恢复实例调度失败", "err", err)
	}

	// ---------------------------------------------------------------
	// 9. HTTP API (gin)
	// ---------------------------------------------------------------
	router := gin.Default()
	handler.NewGameInstanceHandler(gameInstanceUseCase).RegisterRoutes(router)

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
