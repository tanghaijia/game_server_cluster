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

	// node_agent 一键更新（P1，见 docs/node-agent-upgrade-design.md）：发布二进制本地存储目录
	AgentReleaseDir string
	// agent 拉取发布二进制的 controller 下载端点基础地址（node 侧可达，默认本机冒烟；生产配 controller 内网地址）
	AgentUpdateBaseURL string
	// 单节点更新等待心跳回归新版本的超时（秒）
	AgentUpdateWaitSec int

	// 调度器（P1）
	SchedulerScoreWeights      string  // json: {"region":1.0,"bandwidth":0.8,"locality":0.5,"history":0.6,"balance":0.7,"degraded_penalty":2.0,"frequency":0.0,"cache":2.0}
	SchedulerUtilizationTarget float64 // 节点利用率目标（headroom = 1 - target）
	SchedulerRegionForce       bool    // 区域强制（D3）
	SchedulerReservationRetry  int     // 预留冲突重试上限
	SchedulerHistoryWindowSec  int     // 历史评分窗口
	SchedulerHealthStaleSec    int     // 心跳陈旧窗口（H2）
	// P2-C：缓存亲和水位（§5.2，默认 0.8）+ 缓存更新缓冲比例（§8.4，默认 0.15）
	SchedulerCacheSpillWatermark    float64
	SchedulerCacheUpdateBufferRatio float64

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

	// reconcile 稳定性（B-12/B-14，P0）：
	RPCTimeoutSec       int // 单条 node_agent/asset_service RPC 超时（防挂起冻结单消费管线）
	NodeOfflineFenceMin int // 节点失联 fencing 阈值：Running 实例在 unhealthy 节点上失联超此时长 → 置 Failed

	// 排队（P2，§8）
	QueueScanIntervalSec int
	QueueBackoffBaseSec  int
	QueueBackoffMaxSec   int
	QueueTimeoutMin      int
	QueueMaxWakePerRound int

	// game-cache 快照刷新周期（§10，P3）
	CacheViewRefreshSec int

	// P2-B：缓存更新缓冲比例（§8.4）——每节点保留的"下载双倍占用"安全垫，
	// 占该节点缓存磁盘预算的比例；调度/更新共用"可用缓存预算"口径。
	// 默认 0.15（max(最大单分支 size×1.5, cache_budget×15%) 中的 15% 部分）。
	CacheUpdateBufferRatio float64

	// 调度事件缓冲容量（S30 观测）
	EventBufferSize int
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

		// node_agent 一键更新：发布二进制本地目录（默认 ./agent-releases）
		AgentReleaseDir: getEnv("AGENT_RELEASE_DIR", "./agent-releases"),
		// agent 拉取下载端点 base（默认本机 HTTP 端口；生产需配置为 node 可达的 controller 地址）
		AgentUpdateBaseURL: getEnv("AGENT_UPDATE_BASE_URL", "http://127.0.0.1:8090"),
		// 等待心跳回归新版本超时（默认 120s：下载+替换+重启+心跳）
		AgentUpdateWaitSec: getEnvInt("AGENT_UPDATE_WAIT_SEC", 120),

		// 调度器（P1）
		SchedulerScoreWeights:      getEnv("SCHEDULER_SCORE_WEIGHTS", ""),
		SchedulerUtilizationTarget: getEnvFloat("SCHEDULER_UTILIZATION_TARGET", 0.8),
		SchedulerRegionForce:       getEnvBool("SCHEDULER_REGION_FORCE", false),
		SchedulerReservationRetry:  getEnvInt("SCHEDULER_RESERVATION_RETRY", 3),
		SchedulerHistoryWindowSec:  getEnvInt("SCHEDULER_HISTORY_WINDOW_SEC", 900),
		SchedulerHealthStaleSec:    getEnvInt("SCHEDULER_HEALTH_STALE_SEC", 30),
		SchedulerCacheSpillWatermark:    getEnvFloat("SCHEDULER_CACHE_SPILL_WATERMARK", 0.8),
		SchedulerCacheUpdateBufferRatio: getEnvFloat("SCHEDULER_CACHE_UPDATE_BUFFER_RATIO", 0.15),

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

		// reconcile 稳定性（B-12/B-14，P0）
		RPCTimeoutSec:       getEnvInt("RPC_TIMEOUT_SEC", 30),
		NodeOfflineFenceMin: getEnvInt("NODE_OFFLINE_FENCE_MIN", 3),

		// 排队（P2，§8/D9）
		QueueScanIntervalSec: getEnvInt("QUEUE_SCAN_INTERVAL_SEC", 5),
		QueueBackoffBaseSec:  getEnvInt("QUEUE_BACKOFF_BASE_SEC", 15),
		QueueBackoffMaxSec:   getEnvInt("QUEUE_BACKOFF_MAX_SEC", 300),
		QueueTimeoutMin:      getEnvInt("QUEUE_TIMEOUT_MIN", 30),
		QueueMaxWakePerRound: getEnvInt("QUEUE_MAX_WAKE_PER_ROUND", 50),

		// game-cache 快照刷新周期（§10）
		CacheViewRefreshSec: getEnvInt("CACHE_VIEW_REFRESH_SEC", 30),

		// P2-B：缓存更新缓冲比例（§8.4），默认 15%
		CacheUpdateBufferRatio: getEnvFloat("CACHE_UPDATE_BUFFER_RATIO", 0.15),

		// 调度事件缓冲容量（S30）
		EventBufferSize: getEnvInt("EVENT_BUFFER_SIZE", 1000),
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
