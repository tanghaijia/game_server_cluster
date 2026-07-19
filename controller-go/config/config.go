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

	// gRPC
	AssetServiceAddr string

	// HTTP
	HTTPPort int
}

// Load 从环境变量加载配置，未设置时使用默认值
func Load() *Config {
	return &Config{
		DBHost:           getEnv("DB_HOST", "localhost"),
		DBPort:           getEnvInt("DB_PORT", 5432),
		DBUser:           getEnv("DB_USER", "postgres"),
		DBPassword:       getEnv("DB_PASSWORD", "postgres"),
		DBName:           getEnv("DB_NAME", "game_server"),
		AssetServiceAddr: getEnv("ASSET_SERVICE_ADDR", "localhost:9091"),
		HTTPPort:         getEnvInt("HTTP_PORT", 8080),
	}
}

// DSN 返回 PostgreSQL 连接字符串
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName,
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
