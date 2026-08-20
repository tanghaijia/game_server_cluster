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

// TestResolveRequest_ConfigPriority 资源需求优先级（000021 修复）：
// 调度写回的快照（ResourceOverride=false）不覆盖 config 当前值；
// 仅创建时显式指定（ResourceOverride=true）覆盖。
func TestResolveRequest_ConfigPriority(t *testing.T) {
	s := &ResourceAwareScheduler{}
	config := &entity.GameContainerConfig{
		CPURequestMilli: 2000, MemoryRequestBytes: 3 << 30, DiskRequestBytes: 20 << 30,
		BandwidthRxMbps: 80, BandwidthTxMbps: 80,
	}
	// 场景1：实例快照（调度写回，override=false）→ config 当前值优先（config 改 3G 生效）
	inst := &entity.GameInstance{ResourceReq: &entity.ResourceRequest{CPUMilli: 1000, MemoryBytes: 1 << 30}}
	req := s.resolveRequest(inst, config)
	if req.MemoryBytes != 3<<30 {
		t.Errorf("快照不应覆盖 config：内存 = %d, 期望 3GiB", req.MemoryBytes)
	}
	if req.CPUMilli != 2000 {
		t.Errorf("快照不应覆盖 config：cpu = %d, 期望 2000m", req.CPUMilli)
	}
	// 场景2：创建时显式指定（override=true）→ 覆盖 config
	inst2 := &entity.GameInstance{ResourceReq: &entity.ResourceRequest{MemoryBytes: 4 << 30}, ResourceOverride: true}
	req2 := s.resolveRequest(inst2, config)
	if req2.MemoryBytes != 4<<30 {
		t.Errorf("显式指定应覆盖 config：内存 = %d, 期望 4GiB", req2.MemoryBytes)
	}
	if req2.CPUMilli != 2000 {
		t.Errorf("未指定的字段应用 config：cpu = %d, 期望 2000m", req2.CPUMilli)
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

// TestBandwidthRatio 带宽余量评分（§3.5/D6）：headroom = limit − max(预留, 窗口P95占用) 双向取小归一化
func TestBandwidthRatio(t *testing.T) {
	// 上限 1000Mbps，无预留、无占用 → 余量 1.0
	n := &entity.Node{NetRxLimitMbps: 1000, NetTxLimitMbps: 1000}
	if got := bandwidthRatio(n, 0, 0); got != 1.0 {
		t.Errorf("bandwidthRatio 空节点 = %v, 期望 1.0", got)
	}
	// 预留 500Mbps → rx/tx 余量 0.5
	n.BandwidthRxReservedMbps = 500
	n.BandwidthTxReservedMbps = 500
	if got := bandwidthRatio(n, 0, 0); got != 0.5 {
		t.Errorf("bandwidthRatio 预留后 = %v, 期望 0.5", got)
	}
	// 窗口 P95 占用 900Mbps（> 预留）→ rx 余量 0.1；tx 仍 0.5 → 取小 0.1
	if got := bandwidthRatio(n, 900, 0); got != 0.1 {
		t.Errorf("bandwidthRatio P95 占用后 = %v, 期望 0.1", got)
	}
	// 瞬时突发不影响：P95=200（预留 500 更大）→ 0.5
	if got := bandwidthRatio(n, 200, 200); got != 0.5 {
		t.Errorf("bandwidthRatio P95 低于预留 = %v, 期望 0.5", got)
	}
	// 未配置上限 → 0（不影响评分）
	if got := bandwidthRatio(&entity.Node{}, 0, 0); got != 0 {
		t.Errorf("bandwidthRatio 无上限 = %v, 期望 0", got)
	}
}

// TestBandwidthP95 P95 计算
func TestBandwidthP95(t *testing.T) {
	samples := []*entity.NodeResourceSample{
		{NetRxBps: 0}, {NetRxBps: 0}, {NetRxBps: 0},
		{NetRxBps: 900_000_000 / 8}, {NetRxBps: 900_000_000 / 8}, {NetRxBps: 900_000_000 / 8},
	}
	rx, tx := bandwidthP95(samples)
	if rx != 900 { // 900Mbps
		t.Errorf("rx P95 = %v, 期望 900Mbps", rx)
	}
	if tx != 0 {
		t.Errorf("tx P95 = %v, 期望 0", tx)
	}
}
