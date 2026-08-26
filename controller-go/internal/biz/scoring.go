package biz

// 软偏好评分（§6.2）。纯函数、确定性：同输入同输出（S7）。
// 评分只在通过全部硬约束（H1-H6）的候选集内排序选最优。

type ScoreWeights struct {
	Region    float64 `json:"region"`    // 区域偏好（P1）
	Bandwidth float64 `json:"bandwidth"` // 带宽余量（P2）
	Locality  float64 `json:"locality"`  // 数据本地性（P3）
	History   float64 `json:"history"`   // 历史负载趋势（③ 历史视图）
	Balance   float64 `json:"balance"`   // 负载均衡（P4）
	Degraded  float64 `json:"degraded_penalty"` // 降级惩罚（负向）
	Frequency float64 `json:"frequency"` // 单核主频偏好（默认 0 关闭）
	// P2-C：缓存亲和（有可用/下载中缓存且未达水位 → 加分，避免冷启动下载）
	Cache float64 `json:"cache"`
}

func DefaultScoreWeights() ScoreWeights {
	return ScoreWeights{
		Region:    1.0,
		Bandwidth: 0.8,
		Locality:  0.5,
		History:   0.6,
		Balance:   0.7,
		Degraded:  2.0,
		Frequency: 0.0,
		// 强亲和：命中缓存（免一次下载）收益 > 负载均衡微调
		Cache: 2.0,
	}
}

// ScoreInput 评分输入（candidate 组装时预计算；P1 无带宽数据时 BandwidthRatio=0）
type ScoreInput struct {
	RegionMatch   bool
	LastNodeMatch bool
	Utilization   float64 // 0..1（balance，取 max(reserved, used)/capacity，§6.2）
	HistoryUtil   float64 // 0..1（history 窗口均值）
	BandwidthRatio float64 // 0..1（P2）
	Degraded      bool

	// 单核主频（§6.2 可选扩展，single_threaded 实例启用）
	SingleThreaded    bool
	CoreFrequencyGHz  float64
	MinFreqGHz        float64
	MaxFreqGHz        float64

	// P2-C：缓存亲和（节点有该 (game,branch) 可用/下载中缓存，且未达水位）
	CacheAffinity bool
}

// ComputeScore 加权和；parts 返回各维度得分（审计/可解释性 F2）。
func ComputeScore(in ScoreInput, w ScoreWeights) (float64, map[string]float64) {
	parts := make(map[string]float64, 8)
	if in.RegionMatch {
		parts["region"] = 1.0
	}
	if in.LastNodeMatch {
		parts["locality"] = 1.0
	}
	parts["balance"] = 1 - clamp01(in.Utilization)
	parts["history"] = 1 - clamp01(in.HistoryUtil)
	parts["bandwidth"] = clamp01(in.BandwidthRatio)
	if in.CacheAffinity {
		parts["cache"] = 1.0
	}
	if in.Degraded {
		parts["degraded"] = -1.0
	}
	if in.SingleThreaded && in.MaxFreqGHz > in.MinFreqGHz {
		parts["frequency"] = (in.CoreFrequencyGHz - in.MinFreqGHz) / (in.MaxFreqGHz - in.MinFreqGHz)
	}

	score := w.Region*parts["region"] +
		w.Bandwidth*parts["bandwidth"] +
		w.Locality*parts["locality"] +
		w.History*parts["history"] +
		w.Balance*parts["balance"] +
		w.Degraded*parts["degraded"] +
		w.Frequency*parts["frequency"] +
		w.Cache*parts["cache"]
	return score, parts
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
