use crate::prediction_in_out_model::ResourceSnapshot;
use std::time::SystemTime;

/// 获取当前的硬件资源快照
///
/// 通过读取 `/proc` 文件系统采集 CPU、内存、Swap、Load Average 等指标。
pub(crate) fn get_current_snapshot() -> ResourceSnapshot {
    let timestamp_ms = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as i64;

    let (cpu_usage_percent, cpu_cores) = get_cpu_info();
    let (memory_total_mb, memory_available_mb) = get_memory_info();
    let (swap_total_mb, swap_used_mb) = get_swap_info();
    let (load_1m, load_5m) = get_load_average();
    let running_process_count = get_process_count();

    let memory_usage_percent = if memory_total_mb > 0.0 {
        ((memory_total_mb - memory_available_mb) / memory_total_mb * 100.0).max(0.0)
    } else {
        0.0
    };

    ResourceSnapshot {
        timestamp_ms,
        cpu_usage_percent,
        memory_usage_percent,
        memory_total_mb,
        memory_available_mb,
        swap_used_mb,
        swap_total_mb,
        load_average_1m: load_1m,
        load_average_5m: load_5m,
        cpu_cores,
        running_process_count,
    }
}

/// 解析 /proc/stat 获取 CPU 使用率（自启动以来的平均值），
/// 解析 /proc/cpuinfo 获取 CPU 核心数。
fn get_cpu_info() -> (f64, Option<f64>) {
    let stat = std::fs::read_to_string("/proc/stat").unwrap_or_default();
    let first_line = stat.lines().next().unwrap_or("");

    let parts: Vec<&str> = first_line.split_whitespace().collect();
    if parts.len() < 5 || parts[0] != "cpu" {
        return (0.0, None);
    }

    let user: u64 = parts[1].parse().unwrap_or(0);
    let nice: u64 = parts[2].parse().unwrap_or(0);
    let system: u64 = parts[3].parse().unwrap_or(0);
    let idle: u64 = parts[4].parse().unwrap_or(0);
    let iowait: u64 = parts.get(5).and_then(|s| s.parse().ok()).unwrap_or(0);
    let irq: u64 = parts.get(6).and_then(|s| s.parse().ok()).unwrap_or(0);
    let softirq: u64 = parts.get(7).and_then(|s| s.parse().ok()).unwrap_or(0);
    let steal: u64 = parts.get(8).and_then(|s| s.parse().ok()).unwrap_or(0);

    let total = user + nice + system + idle + iowait + irq + softirq + steal;
    let cpu_percent = if total > 0 {
        (1.0 - idle as f64 / total as f64) * 100.0
    } else {
        0.0
    };

    let cores = std::fs::read_to_string("/proc/cpuinfo")
        .unwrap_or_default()
        .lines()
        .filter(|line| line.starts_with("processor"))
        .count();

    let cores = if cores > 0 {
        Some(cores as f64)
    } else {
        None
    };

    (cpu_percent, cores)
}

/// 解析 /proc/meminfo 获取总内存和可用内存（单位 MB）。
///
/// 优先使用 MemAvailable，若内核版本不支持则 fallback 到 MemFree + Cached + Buffers。
fn get_memory_info() -> (f64, f64) {
    let meminfo = std::fs::read_to_string("/proc/meminfo").unwrap_or_default();

    let total_kb = parse_meminfo_key(&meminfo, "MemTotal:");
    let available_kb = parse_meminfo_key(&meminfo, "MemAvailable:");

    let total_mb = total_kb / 1024.0;
    let available_mb = if available_kb > 0.0 {
        available_kb / 1024.0
    } else {
        let free_kb = parse_meminfo_key(&meminfo, "MemFree:");
        let cached_kb = parse_meminfo_key(&meminfo, "Cached:");
        let buffers_kb = parse_meminfo_key(&meminfo, "Buffers:");
        (free_kb + cached_kb + buffers_kb) / 1024.0
    };

    (total_mb, available_mb)
}

/// 解析 /proc/meminfo 获取 Swap 总量和已用量（单位 MB）。
fn get_swap_info() -> (Option<f64>, Option<f64>) {
    let meminfo = std::fs::read_to_string("/proc/meminfo").unwrap_or_default();

    let total_kb = parse_meminfo_key(&meminfo, "SwapTotal:");
    let free_kb = parse_meminfo_key(&meminfo, "SwapFree:");

    if total_kb <= 0.0 {
        return (None, None);
    }

    let total_mb = total_kb / 1024.0;
    let used_mb = (total_kb - free_kb) / 1024.0;

    (Some(total_mb), Some(used_mb.max(0.0)))
}

/// 解析 /proc/loadavg 获取 1 分钟和 5 分钟的 Load Average。
fn get_load_average() -> (Option<f64>, Option<f64>) {
    let loadavg = std::fs::read_to_string("/proc/loadavg").unwrap_or_default();
    let parts: Vec<&str> = loadavg.split_whitespace().collect();

    let load_1m = parts.first().and_then(|s| s.parse::<f64>().ok());
    let load_5m = parts.get(1).and_then(|s| s.parse::<f64>().ok());

    (load_1m, load_5m)
}

/// 解析 /proc/loadavg 获取当前系统进程总数。
///
/// 格式: "0.00 0.00 0.00 1/234 12345"
/// 第四个字段是 running/total 进程数。
fn get_process_count() -> Option<u32> {
    let loadavg = std::fs::read_to_string("/proc/loadavg").unwrap_or_default();
    let parts: Vec<&str> = loadavg.split_whitespace().collect();
    let fourth = parts.get(3).and_then(|s| s.split('/').nth(1));
    fourth.and_then(|s| s.parse::<u32>().ok())
}

/// 从 /proc/meminfo 文本中解析指定 key 的数值（KB）。
fn parse_meminfo_key(meminfo: &str, key: &str) -> f64 {
    for line in meminfo.lines() {
        if line.starts_with(key) {
            let value_str = line
                .trim_start_matches(key)
                .trim()
                .split_whitespace()
                .next()
                .unwrap_or("0");
            return value_str.parse::<f64>().unwrap_or(0.0);
        }
    }
    0.0
}
