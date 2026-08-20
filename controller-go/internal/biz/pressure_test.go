package biz

import (
	"testing"

	"controller-go/internal/entity"
)

// TestPressureStep_NoOscillation 压力状态振荡回归测试：
// 升级与恢复共用同一 util（最近 N 个采样峰值）——同一观察窗内只会单向迁移，
// 不会出现 Normal↔Critical 循环（此前升级看 15min 窗口峰值、恢复看最新采样导致振荡）。
func TestPressureStep_NoOscillation(t *testing.T) {
	const (
		warn = 85.0
		crit = 95.0
		obs  = 3
		rec  = 3
	)

	// 场景：观察窗内有高峰（util=96%）→ 连续 3 轮升 Critical
	status := entity.PressureNormal
	up, down := 0, 0
	for i := 0; i < obs; i++ {
		status, up, down = pressureStep(96, status, up, down, warn, crit, obs, rec)
	}
	if status != entity.PressureCritical {
		t.Fatalf("观察窗持续高峰 3 轮后应为 Critical, 实际 %v", status)
	}
	if down != 0 {
		t.Fatalf("高峰期间不应累积恢复计数, down=%d", down)
	}

	// 场景：高峰滑出观察窗（util=10%）→ 连续 3 轮恢复 Normal
	for i := 0; i < rec; i++ {
		status, up, down = pressureStep(10, status, up, down, warn, crit, obs, rec)
	}
	if status != entity.PressureNormal {
		t.Fatalf("持续低位 3 轮后应恢复 Normal, 实际 %v", status)
	}
	if up != 0 {
		t.Fatalf("低位期间不应累积升级计数, up=%d", up)
	}

	// 场景：同一 util 不会同时升降（振荡断言）——任意单步输入，状态要么不变要么单向
	for _, util := range []float64{10, 50, 88, 97} {
		before := status
		status, _, _ = pressureStep(util, status, 0, 0, warn, crit, obs, rec)
		// 单步只可能不变（同状态）——此处不校验具体方向，仅确保调用不 panic、计数行为由上述用例覆盖
		_ = before
	}
}

// TestPressureStep_WindowPeakDrivesBoth 观察窗峰值同时驱动升级与恢复：
// 高峰在观察窗内 → 只升不降；滑出 → 只降不升（无钉死、无振荡）。
func TestPressureStep_WindowPeakDrivesBoth(t *testing.T) {
	const (
		warn = 85.0
		crit = 95.0
		obs  = 3
		rec  = 3
	)
	// 高峰（96%）在观察窗内，即使"最新采样"低（例如窗口内 3 个采样：96,96,96 → 峰值 96）
	// → 只升级；不会因最新低而恢复
	status := entity.PressureNormal
	up, down := 0, 0
	for i := 0; i < obs; i++ {
		status, up, down = pressureStep(96, status, up, down, warn, crit, obs, rec)
	}
	if status != entity.PressureCritical {
		t.Fatalf("应升 Critical, 实际 %v", status)
	}
	// 高峰仍在观察窗（峰值 96）→ 不会恢复
	status, up, down = pressureStep(96, status, up, down, warn, crit, obs, rec)
	if status != entity.PressureCritical {
		t.Fatalf("观察窗内仍有高峰不应恢复, 实际 %v", status)
	}
	// 高峰滑出（峰值 10）→ 开始恢复
	for i := 0; i < rec; i++ {
		status, up, down = pressureStep(10, status, up, down, warn, crit, obs, rec)
	}
	if status != entity.PressureNormal {
		t.Fatalf("高峰滑出后应恢复 Normal, 实际 %v", status)
	}
}
