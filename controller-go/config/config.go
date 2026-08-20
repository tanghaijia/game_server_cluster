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

	// node_agent 存活检测（心跳探测）
	HeartbeatCheckIntervalSec int
	HeartbeatProbeTimeoutMs   int
	HeartbeatFailThreshold    int

	// 调度器（P1）
	SchedulerScoreWeights      string  // json: {"region":1.0,"bandwidth":0.8,"locality":0.5,"history":0.6,"balance":0.7,"degraded_penalty":2.0,"frequency":0.0}
	SchedulerUtilizationTarget float64 // 节点利用率目标（headroom = 1 - target）
	SchedulerRegionForce       bool    // 区域强制（D3）
	SchedulerReservationRetry  int     // 预留冲突重试上限
	SchedulerHistoryWindowSec  int     // 历史评分窗口
	SchedulerHealthStaleSec    int     // 心跳陈旧窗口（H2）

	// 压力状态机（3.3）
	PressureWarningPct     float64
	PressureCriticalPct    float64
	PressureObservePeriods int
	PressureRecoverPeriods int

	// 健康（9.2）
	HealthDegradedPct float64

	// 中间态卡死哨兵（7.4）
	StaleReservationTimeoutMin int
	StaleReservationScanSec    int

	// 排队（P2，§8）
	QueueScanIntervalSec int
	QueueBackoffBaseSec  int
	QueueBackoffMaxSec   int
	QueueTimeoutMin      int
	QueueMaxWakePerRound int

	// game-cache 快照刷新周期（§10，P3）
	CacheViewRefreshSec int
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
		// 文件服务端口 = node_agent gRPC 端口 + offset。
		// node_agent 文件服务默认监听 50054（= gRPC 50052 + 2，避开 asset_service 的 50053）
		NodeAgentFilePortOffset: getEnvInt("NODE_AGENT_FILE_PORT_OFFSET", 2),

		// node_agent 存活检测
		HeartbeatCheckIntervalSec: getEnvInt("HEARTBEAT_CHECK_INTERVAL_SEC", 10),
		HeartbeatProbeTimeoutMs:   getEnvInt("HEARTBEAT_PROBE_TIMEOUT_MS", 5000),
		// 连续失败多少次才标记失联（防启动/重连瞬态误报）
		HeartbeatFailThreshold: getEnvInt("HEARTBEAT_FAIL_THRESHOLD", 2),

		// 调度器（P1）
		SchedulerScoreWeights:      getEnv("SCHEDULER_SCORE_WEIGHTS", ""),
		SchedulerUtilizationTarget: getEnvFloat("SCHEDULER_UTILIZATION_TARGET", 0.8),
		SchedulerRegionForce:       getEnvBool("SCHEDULER_REGION_FORCE", false),
		SchedulerReservationRetry:  getEnvInt("SCHEDULER_RESERVATION_RETRY", 3),
		SchedulerHistoryWindowSec:  getEnvInt("SCHEDULER_HISTORY_WINDOW_SEC", 900),
		SchedulerHealthStaleSec:    getEnvInt("SCHEDULER_HEALTH_STALE_SEC", 30),

		// 压力状态机（3.3）
		PressureWarningPct:     getEnvFloat("PRESSURE_WARNING_PCT", 85),
		PressureCriticalPct:    getEnvFloat("PRESSURE_CRITICAL_PCT", 95),
		PressureObservePeriods: getEnvInt("PRESSURE_OBSERVE_PERIODS", 3),
		PressureRecoverPeriods: getEnvInt("PRESSURE_RECOVER_PERIODS", 3),

		// 健康（9.2）
		HealthDegradedPct: getEnvFloat("HEALTH_DEGRADED_PCT", 85),

		// 中间态卡死哨兵（7.4）
		StaleReservationTimeoutMin: getEnvInt("STALE_RESERVATION_TIMEOUT_MIN", 10),
		StaleReservationScanSec:    getEnvInt("STALE_RESERVATION_SCAN_SEC", 60),

		// 排队（P2，§8/D9）
		QueueScanIntervalSec: getEnvInt("QUEUE_SCAN_INTERVAL_SEC", 5),
		QueueBackoffBaseSec:  getEnvInt("QUEUE_BACKOFF_BASE_SEC", 15),
		QueueBackoffMaxSec:   getEnvInt("QUEUE_BACKOFF_MAX_SEC", 300),
		QueueTimeoutMin:      getEnvInt("QUEUE_TIMEOUT_MIN", 30),
		QueueMaxWakePerRound: getEnvInt("QUEUE_MAX_WAKE_PER_ROUND", 50),

		// game-cache 快照刷新周期（§10）
		CacheViewRefreshSec: getEnvInt("CACHE_VIEW_REFRESH_SEC", 30),
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

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
