/**
* 算法输入结构体
**/
#[derive(Debug, Clone)]
pub struct PredictionInputModel {
    /// 当前时间戳，单位：毫秒
    pub timestamp_ms: i64,

    /// 当前服务器资源快照
    pub current: ResourceSnapshot,

    /// 最近一段时间的历史采样数据
    ///
    /// 例如最近 5 分钟，每 5 秒一个点，大约 60 个样本。
    pub history: Vec<ResourceSnapshot>,

    /// 新应用进程预计消耗的资源
    pub expected: ExpectedProcessResource,

    /// 预测和决策配置
    pub config: PredictionConfig,
}

/**
* 服务器资源快照结构体
**/
#[derive(Debug, Clone)]
pub struct ResourceSnapshot {
    /// 采样时间戳，单位：毫秒
    pub timestamp_ms: i64,

    /// CPU 使用率，范围 0.0 ~ 100.0
    pub cpu_usage_percent: f64,

    /// 内存使用率，范围 0.0 ~ 100.0
    pub memory_usage_percent: f64,

    /// 总内存，单位 MB
    pub memory_total_mb: f64,

    /// 可用内存，单位 MB
    ///
    /// Linux 上建议使用 MemAvailable，而不是简单的 free memory。
    pub memory_available_mb: f64,

    /// 已使用 swap，单位 MB
    pub swap_used_mb: Option<f64>,

    /// 总 swap，单位 MB
    pub swap_total_mb: Option<f64>,

    /// 系统 load average，例如 1 分钟 load
    pub load_average_1m: Option<f64>,

    /// 系统 load average，例如 5 分钟 load
    pub load_average_5m: Option<f64>,

    /// CPU 核心数
    pub cpu_cores: Option<f64>,

    /// 当前运行中的进程数量
    pub running_process_count: Option<u32>,
}

#[derive(Debug, Clone)]
pub struct ExpectedProcessResource {
    /// 新进程预计 CPU 使用率，范围 0.0 ~ 100.0
    ///
    /// 如果是多核机器，这里建议仍然统一成整机 CPU 百分比。
    /// 例如 8 核机器，一个进程占满 1 核，可以记为 12.5。
    pub cpu_usage_percent: f64,

    /// 新进程预计内存占用，单位 MB
    pub memory_required_mb: f64,

    /// 新进程预计额外 swap 占用，单位 MB
    pub swap_required_mb: Option<f64>,

    /// 新进程启动阶段的 CPU 放大系数
    ///
    /// 有些进程启动瞬间 CPU 较高，可以设为 1.2、1.5、2.0。
    pub startup_cpu_multiplier: f64,

    /// 新进程启动阶段的内存放大系数
    ///
    /// 例如启动时会加载索引、模型、缓存，可以设为 1.2。
    pub startup_memory_multiplier: f64,
}

/**
* 算法配置
**/
#[derive(Debug, Clone)]
pub struct PredictionConfig {
    /// CPU 安全阈值，范围 0.0 ~ 100.0
    ///
    /// 例如 80.0，表示预测启动后 CPU 不应超过 80%。
    pub cpu_threshold_percent: f64,

    /// 内存使用率安全阈值，范围 0.0 ~ 100.0
    ///
    /// 例如 85.0。
    pub memory_threshold_percent: f64,

    /// 必须保留的可用内存，单位 MB
    ///
    /// 比如 1024 MB，防止系统接近 OOM。
    pub reserved_memory_mb: f64,

    /// CPU 安全余量，百分比
    ///
    /// 例如 5.0，表示额外保留 5% CPU。
    pub cpu_margin_percent: f64,

    /// 内存安全余量，单位 MB
    ///
    /// 例如 512 MB。
    pub memory_margin_mb: f64,

    /// EWMA alpha
    ///
    /// 常用 0.1 ~ 0.3。
    pub ewma_alpha: f64,

    /// CPU 分位数
    ///
    /// 一般使用 95.0，代表 P95。
    pub cpu_percentile: f64,

    /// 内存可用量分位数
    ///
    /// 对 memory_available_mb 建议用 5.0，代表 P5。
    pub memory_available_percentile: f64,

    /// CPU 趋势阈值
    ///
    /// 例如 10.0，表示后半窗口平均值比前半窗口高 10% 以上时，认为 CPU 正在明显上升。
    pub cpu_trend_threshold_percent: f64,

    /// 可用内存下降趋势阈值，单位 MB
    ///
    /// 例如 512.0，表示最近窗口中可用内存下降超过 512MB 就认为有风险。
    pub memory_available_drop_threshold_mb: f64,

    /// 是否启用 P95 判断
    pub enable_percentile_check: bool,

    /// 是否启用 EWMA 判断
    pub enable_ewma_check: bool,

    /// 是否启用趋势判断
    pub enable_trend_check: bool,

    /// 是否启用 swap 判断
    pub enable_swap_check: bool,

    /// 当前 CPU 瞬时阻断阈值
    ///
    /// 当前 CPU 超过该值时，即使 EWMA/P95 没有超，也暂缓启动。
    pub instant_cpu_block_threshold_percent: f64,
}

/**
* 算法输出结构体
**/
#[derive(Debug, Clone)]
pub struct PredictionResult {
    /// 是否建议启动新进程
    pub can_start: bool,

    /// 风险等级
    pub risk_level: RiskLevel,

    /// 预测启动后的 CPU 使用率
    pub projected_cpu_percent: f64,

    /// 预测启动后的内存使用率
    pub projected_memory_percent: f64,

    /// 预测启动后的可用内存，单位 MB
    pub projected_memory_available_mb: f64,

    /// CPU EWMA
    pub cpu_ewma: Option<f64>,

    /// 内存使用率 EWMA
    pub memory_usage_ewma: Option<f64>,

    /// CPU P95
    pub cpu_p95: Option<f64>,

    /// 可用内存 P5
    pub memory_available_p5: Option<f64>,

    /// CPU 趋势
    pub cpu_trend_percent: Option<f64>,

    /// 可用内存下降量
    pub memory_available_drop_mb: Option<f64>,

    /// 拒绝或通过的原因
    pub reasons: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RiskLevel {
    Low,
    Medium,
    High,
    Critical,
}


use std::default::Default;

impl Default for PredictionConfig {
    fn default() -> Self {
        Self {
            cpu_threshold_percent: 80.0,
            memory_threshold_percent: 85.0,
            reserved_memory_mb: 1024.0,
            cpu_margin_percent: 5.0,
            memory_margin_mb: 512.0,
            ewma_alpha: 0.2,
            cpu_percentile: 95.0,
            memory_available_percentile: 5.0,
            cpu_trend_threshold_percent: 10.0,
            memory_available_drop_threshold_mb: 512.0,
            enable_percentile_check: true,
            enable_ewma_check: true,
            enable_trend_check: true,
            enable_swap_check: true,
            instant_cpu_block_threshold_percent: 90.0,
        }
    }
}

impl ExpectedProcessResource {
    pub fn new(cpu_usage_percent: f64, memory_required_mb: f64) -> Self {
        Self {
            cpu_usage_percent,
            memory_required_mb,
            swap_required_mb: None,
            startup_cpu_multiplier: 1.0,
            startup_memory_multiplier: 1.0,
        }
    }
}