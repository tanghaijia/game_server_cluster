use crate::prediction_in_out_model::{PredictionInputModel, PredictionResult};
use crate::service::*;

pub fn predict_can_start_process(input: &PredictionInputModel) -> PredictionResult {
    let config = &input.config;
    let expected = &input.expected;

    let mut reasons = Vec::new();

    let expected_cpu = expected.cpu_usage_percent * expected.startup_cpu_multiplier;
    let expected_memory = expected.memory_required_mb * expected.startup_memory_multiplier;

    let cpu_values = collect_cpu_values(input);
    let memory_usage_values = collect_memory_usage_values(input);
    let memory_available_values = collect_memory_available_values(input);

    let cpu_ewma = if config.enable_ewma_check {
        ewma(&cpu_values, config.ewma_alpha)
    } else {
        None
    };

    let memory_usage_ewma = if config.enable_ewma_check {
        ewma(&memory_usage_values, config.ewma_alpha)
    } else {
        None
    };

    let cpu_percentile_value = if config.enable_percentile_check {
        percentile(&cpu_values, config.cpu_percentile)
    } else {
        None
    };

    let memory_available_percentile_value = if config.enable_percentile_check {
        percentile(
            &memory_available_values,
            config.memory_available_percentile,
        )
    } else {
        None
    };

    let cpu_trend_percent = if config.enable_trend_check {
        trend_increase(&cpu_values)
    } else {
        None
    };

    let memory_available_drop_mb = if config.enable_trend_check {
        trend_drop(&memory_available_values)
    } else {
        None
    };

    let (cpu_base, cpu_base_reason) = choose_cpu_base(
        input.current.cpu_usage_percent,
        cpu_ewma,
        cpu_percentile_value,
        config.instant_cpu_block_threshold_percent
    );

    if let Some(reason) = cpu_base_reason {
        reasons.push(reason);
    }

    let memory_usage_base = choose_memory_usage_base(
        input.current.memory_usage_percent,
        memory_usage_ewma,
    );

    let memory_available_base = memory_available_percentile_value
        .unwrap_or(input.current.memory_available_mb);

    let projected_cpu_percent =
        cpu_base + expected_cpu + config.cpu_margin_percent;

    let projected_memory_available_mb =
        memory_available_base - expected_memory - config.memory_margin_mb;

    let projected_memory_percent = calculate_projected_memory_percent(
        input.current.memory_total_mb,
        projected_memory_available_mb,
    );

    let mut can_start = true;

    if projected_cpu_percent > config.cpu_threshold_percent {
        can_start = false;
        reasons.push(format!(
            "CPU 预测过高: projected_cpu={:.2}%, threshold={:.2}%",
            projected_cpu_percent,
            config.cpu_threshold_percent
        ));
    }

    if projected_memory_percent > config.memory_threshold_percent {
        can_start = false;
        reasons.push(format!(
            "内存使用率预测过高: projected_memory={:.2}%, threshold={:.2}%",
            projected_memory_percent,
            config.memory_threshold_percent
        ));
    }

    if projected_memory_available_mb < config.reserved_memory_mb {
        can_start = false;
        reasons.push(format!(
            "可用内存不足: projected_available={:.2}MB, reserved={:.2}MB",
            projected_memory_available_mb,
            config.reserved_memory_mb
        ));
    }

    if let Some(cpu_trend) = cpu_trend_percent {
        if cpu_trend > config.cpu_trend_threshold_percent {
            can_start = false;
            reasons.push(format!(
                "CPU 正在明显上升: trend={:.2}%, threshold={:.2}%",
                cpu_trend,
                config.cpu_trend_threshold_percent
            ));
        }
    }

    if let Some(memory_drop) = memory_available_drop_mb {
        if memory_drop > config.memory_available_drop_threshold_mb {
            can_start = false;
            reasons.push(format!(
                "可用内存正在明显下降: drop={:.2}MB, threshold={:.2}MB",
                memory_drop,
                config.memory_available_drop_threshold_mb
            ));
        }
    }

    if config.enable_swap_check {
        if let Some(reason) = check_swap_risk(&input.current, expected) {
            can_start = false;
            reasons.push(reason);
        }
    }

    if reasons.is_empty() {
        reasons.push("资源余量满足要求，允许启动新进程".to_string());
    }

    let risk_level = classify_risk(
        projected_cpu_percent,
        config.cpu_threshold_percent,
        projected_memory_available_mb,
        config.reserved_memory_mb,
    );

    PredictionResult {
        can_start,
        risk_level,
        projected_cpu_percent,
        projected_memory_percent,
        projected_memory_available_mb,
        cpu_ewma,
        memory_usage_ewma,
        cpu_p95: cpu_percentile_value,
        memory_available_p5: memory_available_percentile_value,
        cpu_trend_percent,
        memory_available_drop_mb,
        reasons,
    }
}