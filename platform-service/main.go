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

	"platform-service/config"
	"platform-service/internal/auth"
	"platform-service/internal/biz"
	"platform-service/internal/client/controller"
	"platform-service/internal/handler"
	repogorm "platform-service/internal/repository/gorm"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// ---------------------------------------------------------------
	// 1. 配置
	// ---------------------------------------------------------------
	cfg := config.Load()
	slog.Info("配置加载完成", "db_host", cfg.DBHost, "db_port", cfg.DBPort, "db_name", cfg.DBName)

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
	userRepo := repogorm.NewUserRepo(db)
	orderRepo := repogorm.NewOrderRepo(db)
	gameProfileRepo := repogorm.NewGameProfileRepo(db)
	serverPlanRepo := repogorm.NewServerPlanRepo(db)
	subscriptionRepo := repogorm.NewSubscriptionRepo(db)

	// ---------------------------------------------------------------
	// 4. Use Cases
	// ---------------------------------------------------------------
	userUseCase := biz.NewUserUseCase(userRepo)

	// controller-go 客户端（ADR-0001）
	controllerClient := controller.NewClient(cfg.ControllerAddr)
	orderUseCase := biz.NewOrderUseCase(orderRepo, controllerClient)
	gameCatalogUseCase := biz.NewGameCatalogUseCase(gameProfileRepo, orderRepo, controllerClient)

	// M9：套餐 / 订阅
	planUseCase := biz.NewPlanUseCase(serverPlanRepo, subscriptionRepo)
	subscriptionUseCase := biz.NewSubscriptionUseCase(subscriptionRepo, planUseCase, controllerClient)

	// 管理员播种（ADR 方案1）：ADMIN_USERNAME/ADMIN_PASSWORD 已设置且用户不存在时创建
	if cfg.AdminUsername != "" {
		_, err := userUseCase.GetUserByName(context.Background(), cfg.AdminUsername)
		switch {
		case err == nil:
			slog.Info("管理员已存在，跳过播种", "username", cfg.AdminUsername)
		case errors.Is(err, gorm.ErrRecordNotFound):
			if _, createErr := userUseCase.CreateAdmin(context.Background(), cfg.AdminUsername, cfg.AdminPassword); createErr != nil {
				slog.Error("创建管理员失败", "username", cfg.AdminUsername, "err", createErr)
			} else {
				slog.Info("管理员已创建", "username", cfg.AdminUsername)
			}
		default:
			slog.Error("查询管理员失败", "username", cfg.AdminUsername, "err", err)
		}
	}

	// JWT 令牌管理（ADR-0004）
	tokenManager := auth.NewTokenManager(
		cfg.JWTSecret,
		time.Duration(cfg.JWTAccessTTLMin)*time.Minute,
		time.Duration(cfg.JWTRefreshTTLHour)*time.Hour,
	)

	// ---------------------------------------------------------------
	// 5. HTTP API (gin)
	// ---------------------------------------------------------------
	router := gin.Default()
	authMiddleware := handler.AuthRequired(tokenManager)
	handler.RegisterHealthRoutes(router)
	handler.NewAuthHandler(userUseCase, tokenManager).RegisterRoutes(router)
	handler.NewUserHandler(userUseCase).RegisterRoutes(router, authMiddleware)
	handler.NewOrderHandler(orderUseCase).RegisterRoutes(router, authMiddleware)
	handler.NewGameCatalogHandler(gameCatalogUseCase).RegisterRoutes(router, authMiddleware)
	handler.NewAdminHandler(controllerClient, gameCatalogUseCase).RegisterRoutes(router, authMiddleware)
	handler.NewAdminPlanHandler(planUseCase, subscriptionUseCase).RegisterRoutes(router, authMiddleware)
	handler.NewSubscriptionHandler(subscriptionUseCase).RegisterRoutes(router, authMiddleware)

	// 静态资源（游戏图标等）
	if err := os.MkdirAll("static", 0o755); err != nil {
		slog.Error("创建 static 目录失败", "err", err)
	}
	router.Static("/static", "./static")

	// M12：到期 sweep（1 分钟周期；进程退出即停止，无需显式取消）
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := subscriptionUseCase.ExpireOverdue(context.Background()); err != nil {
				slog.Warn("到期 sweep 失败", "err", err)
			} else if n > 0 {
				slog.Info("到期 sweep", "expired_subscriptions", n)
			}
		}
	}()

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
	fmt.Printf("Platform-Service 已启动，监听 :%d\n", cfg.HTTPPort)

	// ---------------------------------------------------------------
	// 6. 优雅退出
	// ---------------------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("收到退出信号", "signal", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server 关闭失败", "err", err)
	}
	slog.Info("服务已关闭")
}
