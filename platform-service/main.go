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

	// ---------------------------------------------------------------
	// 4. Use Cases
	// ---------------------------------------------------------------
	userUseCase := biz.NewUserUseCase(userRepo)
	orderUseCase := biz.NewOrderUseCase(orderRepo)

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
