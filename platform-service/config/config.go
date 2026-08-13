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

	// HTTP
	HTTPPort int

	// JWT（ADR-0004）
	JWTSecret         string
	JWTAccessTTLMin   int
	JWTRefreshTTLHour int
}

// Load 从环境变量加载配置，未设置时使用默认值
func Load() *Config {
	return &Config{
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnvInt("DB_PORT", 5432),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", "postgres"),
		DBName:     getEnv("DB_NAME", "platform"),
		DBSchema:   getEnv("DB_SCHEMA", "public"),
		HTTPPort:   getEnvInt("HTTP_PORT", 8081),

		JWTSecret:         getEnv("JWT_SECRET", "dev-secret-change-me"),
		JWTAccessTTLMin:   getEnvInt("JWT_ACCESS_TTL_MIN", 30),
		JWTRefreshTTLHour: getEnvInt("JWT_REFRESH_TTL_HOUR", 168),
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
