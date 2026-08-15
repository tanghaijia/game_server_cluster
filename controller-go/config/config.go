package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config 应用配置，从环境变量读取
type Config struct {
	// PostgreSQL
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	DBSchema   string

	// gRPC
	AssetServiceAddr string

	// HTTP
	HTTPPort int

	// GameCache 后台循环（秒）
	GameCacheReconcileInterval int

	// 实例文件管理（M2）：与 node_agent 共享的 HMAC 密钥 + 文件服务端口偏移
	NodeAgentFileSecret     string
	NodeAgentFilePortOffset int
}

// Load 从环境变量加载配置，未设置时使用默认值
func Load() *Config {
	return &Config{
		DBHost:           getEnv("DB_HOST", "localhost"),
		DBPort:           getEnvInt("DB_PORT", 5432),
		DBUser:           getEnv("DB_USER", "postgres"),
		DBPassword:       getEnv("DB_PASSWORD", "postgres"),
		DBName:           getEnv("DB_NAME", "game_server"),
		DBSchema:         getEnv("DB_SCHEMA", "public"),
		AssetServiceAddr: getEnv("ASSET_SERVICE_ADDR", "localhost:9091"),
		HTTPPort:         getEnvInt("HTTP_PORT", 8090),
		// 默认 60 秒执行一轮分支同步 + 缓存检查
		GameCacheReconcileInterval: getEnvInt("GAME_CACHE_RECONCILE_INTERVAL", 60),

		NodeAgentFileSecret:     getEnv("NODE_AGENT_FILE_SECRET", "dev-file-secret-change-me"),
		NodeAgentFilePortOffset: getEnvInt("NODE_AGENT_FILE_PORT_OFFSET", 1),
	}
}

// DSN 返回 PostgreSQL 连接字符串
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSchema,
	)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
