package biz

import (
	"math"
	"strings"
	"testing"

	"controller-go/internal/entity"
)

// TestComputeCapacity_Sequential §7.3 连续调度算例：
// 4 核 / 16GiB / utilization_target=0.8 → A(2000m/4GiB) 可调度，B(2000m/4GiB) CPU 不足（缺口 800m）。
func TestComputeCapacity_Sequential(t *testing.T) {
	node := &entity.Node{CoreNum: 4, MemorySize: 16384, StorageSize: 102400}
	req := entity.ResourceRequest{CPUMilli: 2000, MemoryBytes: 4 << 30, DiskBytes: 10 << 30}

	// A：空节点，allocatable(cpu)=3200m ≥ 2000m → 可调度
	capA := ComputeCapacity(node, 0.8)
	if !capA.ResourcesSatisfied(req) {
		t.Fatalf("A 应可调度: %+v", capA)
	}

	// 扣减 A 的预留（调度事务内动作）
	node.CPUReservedMilli += req.CPUMilli
	node.MemoryReservedBytes += req.MemoryBytes
	node.DiskReservedBytes += req.DiskBytes

	// B：allocatable(cpu) = 3200 − 2000 = 1200 < 2000 → CPU 不足
	capB := ComputeCapacity(node, 0.8)
	if capB.ResourcesSatisfied(req) {
		t.Fatalf("B 不应可调度（CPU 已不足）: %+v", capB)
	}
	shortages := capB.Shortages(req)
	if len(shortages) == 0 || !strings.Contains(shortages[0], "cpu 不足") {
		t.Fatalf("应报告 CPU 缺口: %v", shortages)
	}
	// 内存仍充足：12.8GiB − 4GiB = 8.8GiB ≥ 4GiB
	if capB.MemAllocatableBytes < req.MemoryBytes {
		t.Fatalf("内存应仍充足: %+v", capB)
	}
}

// TestComputeScore_Example62 §6.2 评分算例：
// N1(sg, 余量0.5, 利用率0.6, healthy) / N2(us, 余量0.8, locality, 利用率0.2) /
// N3(sg, 余量0.3, 利用率0.1, degraded) → 选 N2（区域不匹配仍胜出，体现 D3 偏好语义）。
// 注：HistoryUtil=0（窗口无采样），history 项每节点贡献 w_history。
func TestComputeScore_Example62(t *testing.T) {
	w := DefaultScoreWeights()

	cases := []struct {
		name string
		in   ScoreInput
		want float64
	}{
		{"N1", ScoreInput{RegionMatch: true, BandwidthRatio: 0.5, Utilization: 0.6}, 2.28},
		{"N2", ScoreInput{BandwidthRatio: 0.8, LastNodeMatch: true, Utilization: 0.2}, 2.30},
		{"N3", ScoreInput{RegionMatch: true, BandwidthRatio: 0.3, Utilization: 0.1, Degraded: true}, 0.47},
	}
	var maxName string
	var maxScore float64
	for _, c := range cases {
		score, parts := ComputeScore(c.in, w)
		if math.Abs(score-c.want) > 1e-9 {
			t.Errorf("%s 得分 = %v, 期望 %v (parts=%v)", c.name, score, c.want, parts)
		}
		if score > maxScore {
			maxScore = score
			maxName = c.name
		}
	}
	if maxName != "N2" {
		t.Errorf("应选 N2, 实际选择 %s (score=%v)", maxName, maxScore)
	}
}

// TestComputeScore_Deterministic 确定性（S7）：同输入同输出
func TestComputeScore_Deterministic(t *testing.T) {
	w := DefaultScoreWeights()
	in := ScoreInput{RegionMatch: true, BandwidthRatio: 0.6, LastNodeMatch: true, Utilization: 0.5, HistoryUtil: 0.3}
	s1, _ := ComputeScore(in, w)
	s2, _ := ComputeScore(in, w)
	if s1 != s2 {
		t.Fatalf("确定性被破坏: %v != %v", s1, s2)
	}
}

// TestCapacityLimit 单位换算与 headroom
func TestCapacityLimit(t *testing.T) {
	node := &entity.Node{CoreNum: 4, MemorySize: 16384}
	if got := cpuCapacityMilli(node); got != 4000 {
		t.Errorf("cpuCapacityMilli = %d, 期望 4000", got)
	}
	if got := memoryCapacityBytes(node); got != 16<<30 {
		t.Errorf("memoryCapacityBytes = %d, 期望 16GiB", got)
	}
	if got := capacityLimit(cpuCapacityMilli(node), 0.8); got != 3200 {
		t.Errorf("capacityLimit = %d, 期望 3200", got)
	}
}

// TestBandwidthRatio 带宽余量评分（§3.5/D6）：limit − max(预留, 当前bps) 双向取小归一化
func TestBandwidthRatio(t *testing.T) {
	// 上限 1000Mbps，无预留、无占用 → 余量 1.0
	n := &entity.Node{NetRxLimitMbps: 1000, NetTxLimitMbps: 1000}
	if got := bandwidthRatio(n); got != 1.0 {
		t.Errorf("bandwidthRatio 空节点 = %v, 期望 1.0", got)
	}
	// 预留 500Mbps → rx/tx 余量 0.5
	n.BandwidthRxReservedMbps = 500
	n.BandwidthTxReservedMbps = 500
	if got := bandwidthRatio(n); got != 0.5 {
		t.Errorf("bandwidthRatio 预留后 = %v, 期望 0.5", got)
	}
	// 当前占用 900Mbps（> 预留）→ rx 余量 0.1；tx 仍 0.5 → 取小 0.1
	n.NetRxBps = 900_000_000 / 8 // 900Mbps = 112.5MB/s（bps→Mbps 换算 ×8/1e6）
	if got := bandwidthRatio(n); got != 0.1 {
		t.Errorf("bandwidthRatio 占用后 = %v, 期望 0.1", got)
	}
	// 未配置上限 → 0（不影响评分）
	if got := bandwidthRatio(&entity.Node{}); got != 0 {
		t.Errorf("bandwidthRatio 无上限 = %v, 期望 0", got)
	}
}
