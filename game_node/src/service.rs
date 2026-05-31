use crate::prediction_in_out_model::{ExpectedProcessResource, PredictionInputModel, ResourceSnapshot, RiskLevel};

pub(crate) fn collect_cpu_values(input: &PredictionInputModel) -> Vec<f64> {
    let mut values: Vec<f64> = input
        .history
        .iter()
        .map(|x| x.cpu_usage_percent)
        .collect();

    values.push(input.current.cpu_usage_percent);
    values
}

pub(crate) fn collect_memory_usage_values(input: &PredictionInputModel) -> Vec<f64> {
    let mut values: Vec<f64> = input
        .history
        .iter()
        .map(|x| x.memory_usage_percent)
        .collect();

    values.push(input.current.memory_usage_percent);
    values
}

pub(crate) fn collect_memory_available_values(input: &PredictionInputModel) -> Vec<f64> {
    let mut values: Vec<f64> = input
        .history
        .iter()
        .map(|x| x.memory_available_mb)
        .collect();

    values.push(input.current.memory_available_mb);
    values
}

pub(crate) fn ewma(values: &[f64], alpha: f64) -> Option<f64> {
    if values.is_empty() {
        return None;
    }

    if alpha <= 0.0 || alpha > 1.0 {
        return None;
    }

    let mut result = values[0];

    for value in values.iter().skip(1) {
        result = alpha * value + (1.0 - alpha) * result;
    }

    Some(result)
}

pub(crate) fn percentile(values: &[f64], percentile: f64) -> Option<f64> {
    if values.is_empty() {
        return None;
    }

    if percentile < 0.0 || percentile > 100.0 {
        return None;
    }

    let mut sorted = values.to_vec();

    sorted.sort_by(|a, b| {
        a.partial_cmp(b)
            .unwrap_or(std::cmp::Ordering::Equal)
    });

    if sorted.len() == 1 {
        return Some(sorted[0]);
    }

    let rank = percentile / 100.0 * (sorted.len() - 1) as f64;
    let lower_index = rank.floor() as usize;
    let upper_index = rank.ceil() as usize;

    if lower_index == upper_index {
        return Some(sorted[lower_index]);
    }

    let lower_value = sorted[lower_index];
    let upper_value = sorted[upper_index];
    let weight = rank - lower_index as f64;

    Some(lower_value + (upper_value - lower_value) * weight)
}

pub(crate) fn trend_increase(values: &[f64]) -> Option<f64> {
    if values.len() < 4 {
        return None;
    }

    let mid = values.len() / 2;

    let first_half_avg = average(&values[..mid])?;
    let second_half_avg = average(&values[mid..])?;

    Some(second_half_avg - first_half_avg)
}

pub(crate) fn trend_drop(values: &[f64]) -> Option<f64> {
    if values.len() < 4 {
        return None;
    }

    let mid = values.len() / 2;

    let first_half_avg = average(&values[..mid])?;
    let second_half_avg = average(&values[mid..])?;

    Some(first_half_avg - second_half_avg)
}

pub(crate) fn average(values: &[f64]) -> Option<f64> {
    if values.is_empty() {
        return None;
    }

    let sum: f64 = values.iter().sum();
    Some(sum / values.len() as f64)
}

/// EWMA、分位数中选择 CPU 基准值（取最大值）。
pub(crate) fn choose_cpu_base(
    current_cpu: f64,
    cpu_ewma: Option<f64>,
    cpu_percentile_value: Option<f64>,
    instant_block_threshold: f64,
) -> (f64, Option<String>) {
    let mut base = current_cpu;

    if let Some(value) = cpu_ewma {
        base = value;
    }

    if let Some(value) = cpu_percentile_value {
        base = base.max(value);
    }

    if current_cpu > instant_block_threshold {
        return (
            base,
            Some(format!(
                "当前 CPU 瞬时过高: current_cpu={:.2}%, threshold={:.2}%",
                current_cpu, instant_block_threshold
            )),
        );
    }

    (base, None)
}

/// 从当前值和 EWMA 中选择内存使用率基准值（取最大值）。
pub(crate) fn choose_memory_usage_base(
    current_memory: f64,
    memory_ewma: Option<f64>,
) -> f64 {
    let mut base = current_memory;
    if let Some(ewma_val) = memory_ewma {
        base = base.max(ewma_val);
    }
    base
}

/// 根据总内存和预测可用内存计算内存使用率百分比。
pub(crate) fn calculate_projected_memory_percent(
    memory_total_mb: f64,
    projected_memory_available_mb: f64,
) -> f64 {
    if memory_total_mb <= 0.0 {
        return 0.0;
    }
    let used = memory_total_mb - projected_memory_available_mb;
    (used / memory_total_mb * 100.0).max(0.0)
}

/// 检查 swap 风险。
pub(crate) fn check_swap_risk(
    current: &ResourceSnapshot,
    expected: &ExpectedProcessResource,
) -> Option<String> {
    let swap_total = current.swap_total_mb?;
    if swap_total <= 0.0 {
        return None;
    }

    let swap_used = current.swap_used_mb.unwrap_or(0.0);
    let expected_swap = expected.swap_required_mb.unwrap_or(0.0);

    let projected_swap_used = swap_used + expected_swap;
    let swap_usage_ratio = projected_swap_used / swap_total;

    let swap_warning_threshold = 0.8;

    if swap_usage_ratio > swap_warning_threshold {
        Some(format!(
            "Swap 占用过高: projected_swap={:.2}MB, total_swap={:.2}MB, ratio={:.2}%",
            projected_swap_used,
            swap_total,
            swap_usage_ratio * 100.0
        ))
    } else {
        None
    }
}

/// 综合评估风险等级。
pub(crate) fn classify_risk(
    projected_cpu_percent: f64,
    cpu_threshold_percent: f64,
    projected_memory_available_mb: f64,
    reserved_memory_mb: f64,
) -> RiskLevel {
    let cpu_ratio = projected_cpu_percent / cpu_threshold_percent;
    let memory_ratio = if reserved_memory_mb > 0.0 {
        1.0 - projected_memory_available_mb / reserved_memory_mb
    } else {
        0.0
    };

    if cpu_ratio >= 1.0 || memory_ratio >= 1.0 {
        RiskLevel::Critical
    } else if cpu_ratio >= 0.85 || memory_ratio >= 0.8 {
        RiskLevel::High
    } else if cpu_ratio >= 0.7 || memory_ratio >= 0.5 {
        RiskLevel::Medium
    } else {
        RiskLevel::Low
    }
}
