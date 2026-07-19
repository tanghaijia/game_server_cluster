package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"controller-go/config"
	"controller-go/internal/biz"
	"controller-go/internal/client/assetservice"
	nodeagentclient "controller-go/internal/client/nodeagent"
	repogorm "controller-go/internal/repository/gorm"

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

	// 自动建表
	if err := db.AutoMigrate(
		&GormGame{},
		&GormNode{},
		&GormNodeAgent{},
		&GormGameInstance{},
	); err != nil {
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
	slog.Info("AssetService 客户端创建成功", "addr", cfg.AssetServiceAddr)

	// NodeAgent (按需懒加载，通过 ClientRegistry 管理)
	nodeAgentClients := nodeagentclient.NewClientRegistry()
	defer nodeAgentClients.CloseAll()

	// ---------------------------------------------------------------
	// 5. Scheduler
	// ---------------------------------------------------------------
	scheduler := biz.NewSimpleScheduler(nil)

	// ---------------------------------------------------------------
	// 6. ReconcileDispatcher
	// ---------------------------------------------------------------
	dispatcher := biz.NewReconcileDispatcher(
		gameInstanceRepo,
		nodeAgentRepo,
		nodeRepo,
		scheduler,
		nodeAgentClients,
	)

	// ---------------------------------------------------------------
	// 7. Use Cases
	// ---------------------------------------------------------------
	_ = biz.NewGameUseCase(gameRepo)
	_ = biz.NewGameInstanceUseCase(gameInstanceRepo, dispatcher)
	_ = biz.NewGameInstanceAdvanceUseCase(scheduler, gameInstanceRepo, assetClient)
	_ = biz.NewNodeUseCase(nodeRepo)
	_ = biz.NewNodeAgentUseCase(nodeAgentRepo, nodeRepo)

	// ---------------------------------------------------------------
	// 8. 启动 dispatcher
	// ---------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dispatcher.Start(ctx)
	slog.Info("ReconcileDispatcher 已启动")

	// ---------------------------------------------------------------
	// 9. HTTP / 监控 (可选)
	// ---------------------------------------------------------------
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
	slog.Info("服务已关闭")
}

// ---------------------------------------------------------------
// GORM 模型（避免与 entity 耦合）
// ---------------------------------------------------------------

type GormGame struct {
	ID   string `gorm:"column:id;primaryKey"`
	Name string `gorm:"column:name"`
}

func (GormGame) TableName() string { return "games" }

type GormNode struct {
	Id              int64   `gorm:"column:id;primaryKey;autoIncrement"`
	Ip              string  `gorm:"column:ip"`
	CoreNum         int     `gorm:"column:core_num"`
	CoreFrequency   float64 `gorm:"column:core_frequency"`
	MemorySize      int64   `gorm:"column:memory_size"`
	StorageSize     int64   `gorm:"column:storage_size"`
	Location        string  `gorm:"column:location"`
	ServiceProvider string  `gorm:"column:service_provider"`
}

func (GormNode) TableName() string { return "nodes" }

type GormNodeAgent struct {
	ID     string `gorm:"column:id;primaryKey"`
	NodeId string `gorm:"column:node_id"`
	Port   int32  `gorm:"column:port"`
}

func (GormNodeAgent) TableName() string { return "node_agents" }

type GormGameInstance struct {
	ID              string    `gorm:"column:id;primaryKey"`
	GameID          string    `gorm:"column:game_id"`
	NodeAgentID     *string   `gorm:"column:node_agent_id"`
	Status          int       `gorm:"column:status"`
	LastPendingTime time.Time `gorm:"column:last_pending_time"`
	CreateTime      time.Time `gorm:"column:create_time"`
	UpdateTime      time.Time `gorm:"column:update_time"`
	GameBuildId     string    `gorm:"column:game_build_id"`
}

func (GormGameInstance) TableName() string { return "game_instances" }
