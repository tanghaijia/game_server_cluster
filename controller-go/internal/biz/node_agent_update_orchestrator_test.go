package biz

import (
	"context"
	"testing"
	"time"

	"controller-go/internal/entity"

	"gorm.io/gorm"
)

// ---- orchestrator 依赖的轻量 fake ----

type fakeUpdateAgentRepo struct {
	agents map[string]*entity.NodeAgent
}

func newFakeUpdateAgentRepo(agents ...*entity.NodeAgent) *fakeUpdateAgentRepo {
	m := map[string]*entity.NodeAgent{}
	for _, a := range agents {
		m[a.ID] = a
	}
	return &fakeUpdateAgentRepo{agents: m}
}

func (f *fakeUpdateAgentRepo) Save(_ context.Context, a *entity.NodeAgent) error { f.agents[a.ID] = a; return nil }
func (f *fakeUpdateAgentRepo) GetByID(_ context.Context, id string) (*entity.NodeAgent, error) {
	if a, ok := f.agents[id]; ok {
		return a, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (f *fakeUpdateAgentRepo) ListEnabledIDs(context.Context) ([]string, error) { return nil, nil }
func (f *fakeUpdateAgentRepo) ListAll(context.Context) ([]*entity.NodeAgent, error) { return nil, nil }
func (f *fakeUpdateAgentRepo) UpdateHealth(context.Context, string, bool, time.Time) error {
	return nil
}
func (f *fakeUpdateAgentRepo) UpdateHealthStatus(context.Context, string, entity.NodeAgentHealthStatus) error {
	return nil
}
func (f *fakeUpdateAgentRepo) UpdateAgentVersion(_ context.Context, id, v string) error {
	if a, ok := f.agents[id]; ok {
		a.AgentVersion = v
	}
	return nil
}
func (f *fakeUpdateAgentRepo) UpdateUpdateState(_ context.Context, id, state, target, errMsg string) error {
	if a, ok := f.agents[id]; ok {
		a.UpdateState = state
		a.TargetVersion = target
		a.LastUpdateErr = errMsg
	}
	return nil
}

// fakeActiveInstancesRepo 返回给定 agent 上的活跃实例（用于前置过滤）
type fakeActiveInstancesRepo struct {
	active []*entity.GameInstance
}

func (f *fakeActiveInstancesRepo) Save(context.Context, *entity.GameInstance) error { return nil }
func (f *fakeActiveInstancesRepo) GetByID(context.Context, string) (*entity.GameInstance, error) {
	return nil, gorm.ErrRecordNotFound
}
func (f *fakeActiveInstancesRepo) UpdateStatus(context.Context, *entity.GameInstance) error { return nil }
func (f *fakeActiveInstancesRepo) ListByStatuses(context.Context, ...entity.InstanceStatus) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (f *fakeActiveInstancesRepo) ListByGame(context.Context, string) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (f *fakeActiveInstancesRepo) ListActiveBySubscription(context.Context, string) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (f *fakeActiveInstancesRepo) ListBySubscription(context.Context, string) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (f *fakeActiveInstancesRepo) ListAll(context.Context) ([]*entity.GameInstance, error) {
	return f.active, nil
}
func (f *fakeActiveInstancesRepo) Delete(context.Context, string) error { return nil }

func TestIsActiveInstanceStatus(t *testing.T) {
	active := []entity.InstanceStatus{
		entity.StatusPending, entity.StatusScheduling, entity.StatusRunning, entity.StatusQueued,
		entity.StatusCacheWarming, entity.StatusStarting, entity.StatusStopping,
	}
	for _, s := range active {
		if !isActiveInstanceStatus(s) {
			t.Errorf("status %v should be active", s)
		}
	}
	inactive := []entity.InstanceStatus{entity.StatusStopped, entity.Failed}
	for _, s := range inactive {
		if isActiveInstanceStatus(s) {
			t.Errorf("status %v should be inactive", s)
		}
	}
}

// 前置过滤：已是最新版本 → skipped + OK；失联 → skipped；release 不存在 → 失败
func TestOrchestratorPreFilter(t *testing.T) {
	agentRepo := newFakeUpdateAgentRepo(&entity.NodeAgent{
		ID: "agent-1", NodeId: "n1", Port: 9090, Alive: true,
		AgentVersion: "v0.1.0", UpdateState: "idle",
	})
	releaseRepo := newFakeAgentReleaseRepo()
	rel := &entity.AgentRelease{ID: "rel-1", Version: "v0.1.1", OS: "linux", Arch: "amd64",
		SHA256: "abc", SizeBytes: 1}
	_ = releaseRepo.Save(context.Background(), rel)

	o := &NodeAgentUpdateOrchestrator{
		nodeAgentRepo: agentRepo,
		instanceRepo:  &fakeActiveInstancesRepo{},
		releaseRepo:   releaseRepo,
		waitTimeout:   time.Second,
		pollEvery:     100 * time.Millisecond,
	}

	// 场景 1：release 不存在
	res := o.updateOne(context.Background(), NodeAgentUpdateTarget{AgentID: "agent-1", ReleaseID: "nope"})
	if res.OK || res.Reason == "" {
		t.Errorf("case release-missing: want failure reason, got %+v", res)
	}
	// 场景 2：已是目标版本（改 agent 当前版本 = v0.1.1）
	agentRepo.agents["agent-1"].AgentVersion = "v0.1.1"
	res = o.updateOne(context.Background(), NodeAgentUpdateTarget{AgentID: "agent-1", ReleaseID: "rel-1"})
	if !res.Skipped || !res.OK {
		t.Errorf("case already-latest: want skipped+ok, got %+v", res)
	}
	// 场景 3：失联
	agentRepo.agents["agent-1"].AgentVersion = "v0.1.0"
	agentRepo.agents["agent-1"].Alive = false
	res = o.updateOne(context.Background(), NodeAgentUpdateTarget{AgentID: "agent-1", ReleaseID: "rel-1"})
	if !res.Skipped || res.Reason == "" || !containsStr(res.Reason, "失联") {
		t.Errorf("case dead: want skipped(失联), got %+v", res)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
