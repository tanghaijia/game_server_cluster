package biz

import "testing"

// B-04/P1-1：RuntimeStatsRegistry 按节点作用域更新与清理
func TestRuntimeStatsRegistry_UpdateAndPrune(t *testing.T) {
	reg := NewRuntimeStatsRegistry()

	// 节点 A 上报两个实例
	reg.UpdateForNode("agent-a", []InstanceRuntimeStat{
		{InstanceID: "inst-1", PlayerCount: 3, MaxPlayers: 8, Healthy: true, ProbeMode: "script"},
		{InstanceID: "inst-2", PlayerCount: 0, MaxPlayers: 8, Healthy: false, ProbeError: "进程不健康"},
	})
	// 节点 B 上报一个实例（与 A 互不影响）
	reg.UpdateForNode("agent-b", []InstanceRuntimeStat{
		{InstanceID: "inst-9", PlayerCount: 1, MaxPlayers: 4, Healthy: true, ProbeMode: "a2s"},
	})

	s, ok := reg.Get("inst-1")
	if !ok || s.PlayerCount != 3 || !s.Healthy {
		t.Fatalf("inst-1 应存在且数据正确, ok=%v stat=%+v", ok, s)
	}
	if s, ok := reg.Get("inst-9"); !ok || s.ProbeMode != "a2s" {
		t.Fatalf("inst-9（节点 B）应保留, ok=%v", ok)
	}

	// 节点 A 下次上报只含 inst-1（inst-2 停止）→ inst-2 被清除
	reg.UpdateForNode("agent-a", []InstanceRuntimeStat{
		{InstanceID: "inst-1", PlayerCount: 5, MaxPlayers: 8, Healthy: true},
	})
	if _, ok := reg.Get("inst-2"); ok {
		t.Fatal("inst-2 不再上报应被清除")
	}
	// 节点 B 的 inst-9 不受节点 A 更新影响
	if _, ok := reg.Get("inst-9"); !ok {
		t.Fatal("节点 B 实例不应被节点 A 的更新清除")
	}

	// 节点 A 上报空列表 → 其全部实例清除；节点 B 仍保留
	reg.UpdateForNode("agent-a", nil)
	if _, ok := reg.Get("inst-1"); ok {
		t.Fatal("节点 A 上报空列表后 inst-1 应被清除")
	}
	if _, ok := reg.Get("inst-9"); !ok {
		t.Fatal("inst-9 应仍存在")
	}
}

func TestRuntimeStatsRegistry_GetMissing(t *testing.T) {
	reg := NewRuntimeStatsRegistry()
	if _, ok := reg.Get("no-such"); ok {
		t.Fatal("不存在的实例应返回 ok=false")
	}
}
