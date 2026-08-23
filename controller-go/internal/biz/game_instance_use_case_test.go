package biz

import (
	"context"
	"errors"
	"sync"
	"testing"

	"controller-go/internal/entity"
	"controller-go/internal/repository"

	"gorm.io/gorm"
)

// slotMockInstanceRepo 仅实现 StartGameInstance/checkSubscriptionSlot 需要的路径
type slotMockInstanceRepo struct {
	getByIDFunc func(ctx context.Context, id string) (*entity.GameInstance, error)
	listActive  func(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error)
	saveFunc    func(ctx context.Context, inst *entity.GameInstance) error
	saveCount   int
}

func (m *slotMockInstanceRepo) Save(ctx context.Context, inst *entity.GameInstance) error {
	m.saveCount++
	if m.saveFunc != nil {
		return m.saveFunc(ctx, inst)
	}
	return nil
}
func (m *slotMockInstanceRepo) GetByID(ctx context.Context, id string) (*entity.GameInstance, error) {
	return m.getByIDFunc(ctx, id)
}
func (m *slotMockInstanceRepo) UpdateStatus(ctx context.Context, inst *entity.GameInstance) error {
	return m.Save(ctx, inst)
}
func (m *slotMockInstanceRepo) ListByStatuses(ctx context.Context, statuses ...entity.InstanceStatus) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (m *slotMockInstanceRepo) ListAll(ctx context.Context) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (m *slotMockInstanceRepo) ListByGame(ctx context.Context, gameID string) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (m *slotMockInstanceRepo) ListActiveBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error) {
	if m.listActive != nil {
		return m.listActive(ctx, subscriptionID)
	}
	return nil, nil
}
func (m *slotMockInstanceRepo) ListBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (m *slotMockInstanceRepo) Delete(ctx context.Context, id string) error { return nil }

// newSlotUseCase 构造最小 GameInstanceUseCase：仅 instanceRepo + 可用的 dispatch 队列
func newSlotUseCase(repo repository.GameInstanceRepository) *GameInstanceUseCase {
	return &GameInstanceUseCase{
		instanceRepo: repo,
		ReconcileDispatcher: &ReconcileDispatcher{
			queue: make(chan *entity.GameInstance, 8),
		},
	}
}

func strPtr(s string) *string { return &s }

func stoppedInstance(id, sub string) *entity.GameInstance {
	return &entity.GameInstance{ID: id, GameID: "343050", Status: entity.StatusStopped, SubscriptionID: strPtr(sub)}
}

// TestStartGameInstance_SubscriptionBusy 订阅内已有其他活跃实例 → 拒绝（ErrSubscriptionConflict）
func TestStartGameInstance_SubscriptionBusy(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return stoppedInstance("inst-a", "sub-1"), nil
		},
		listActive: func(ctx context.Context, sub string) ([]*entity.GameInstance, error) {
			if sub != "sub-1" {
				t.Fatalf("expected query on sub-1, got %q", sub)
			}
			return []*entity.GameInstance{{ID: "inst-b", GameID: "294420", Status: entity.StatusRunning, SubscriptionID: strPtr("sub-1")}}, nil
		},
	}
	uc := newSlotUseCase(repo)

	err := uc.StartGameInstance(context.Background(), "inst-a")
	if !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("expected ErrSubscriptionConflict, got %v", err)
	}
	if repo.saveCount != 0 {
		t.Fatalf("conflicting start must not persist, saveCount=%d", repo.saveCount)
	}
}

// TestStartGameInstance_NoConflict_TransitionsToPending 无冲突 → pending + 入队
func TestStartGameInstance_NoConflict_TransitionsToPending(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return stoppedInstance("inst-a", "sub-1"), nil
		},
		listActive: func(ctx context.Context, sub string) ([]*entity.GameInstance, error) {
			return nil, nil
		},
	}
	uc := newSlotUseCase(repo)

	if err := uc.StartGameInstance(context.Background(), "inst-a"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if repo.saveCount != 1 {
		t.Fatalf("expected 1 save, got %d", repo.saveCount)
	}
	select {
	case inst := <-uc.ReconcileDispatcher.queue:
		if inst.Status != entity.StatusPending {
			t.Fatalf("dispatched instance should be pending, got %s", inst.Status)
		}
	default:
		t.Fatal("instance should have been dispatched")
	}
}

// TestStartGameInstance_NoSubscription_Exempt 未归属订阅（老实例）→ 不查询、直接放行
func TestStartGameInstance_NoSubscription_Exempt(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return &entity.GameInstance{ID: "inst-legacy", GameID: "343050", Status: entity.StatusStopped}, nil
		},
		// 若被调用说明未归属实例也走了订阅查询（应豁免）
		listActive: func(ctx context.Context, sub string) ([]*entity.GameInstance, error) {
			t.Fatalf("ListActiveBySubscription should not be called for legacy instance")
			return nil, nil
		},
	}
	uc := newSlotUseCase(repo)
	if err := uc.StartGameInstance(context.Background(), "inst-legacy"); err != nil {
		t.Fatalf("start legacy instance: %v", err)
	}
}

// TestStartGameInstance_SelfInActiveList_Idempotent 活跃列表含自己 → 幂等放行
func TestStartGameInstance_SelfInActiveList_Idempotent(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return stoppedInstance("inst-a", "sub-1"), nil
		},
		listActive: func(ctx context.Context, sub string) ([]*entity.GameInstance, error) {
			// 理论不可能，但防御：活跃列表里出现自己（同 ID）不应视为冲突
			return []*entity.GameInstance{{ID: "inst-a", GameID: "343050", Status: entity.StatusStarting, SubscriptionID: strPtr("sub-1")}}, nil
		},
	}
	uc := newSlotUseCase(repo)
	if err := uc.StartGameInstance(context.Background(), "inst-a"); err != nil {
		t.Fatalf("start with self in active list should pass, got %v", err)
	}
}

// TestCheckSubscriptionSlot_NoOtherActive 仅有自己（或空列表）→ 通过
func TestCheckSubscriptionSlot_NoOtherActive(t *testing.T) {
	repo := &slotMockInstanceRepo{listActive: func(ctx context.Context, sub string) ([]*entity.GameInstance, error) {
		return []*entity.GameInstance{{ID: "inst-a", GameID: "343050", Status: entity.StatusStarting, SubscriptionID: strPtr("sub-1")}}, nil
	}}
	uc := newSlotUseCase(repo)

	if err := uc.checkSubscriptionSlot(context.Background(), stoppedInstance("inst-a", "sub-1")); err != nil {
		t.Fatalf("self in active list should pass, got %v", err)
	}
	if err := uc.checkSubscriptionSlot(context.Background(), &entity.GameInstance{ID: "x", SubscriptionID: nil}); err != nil {
		t.Fatalf("nil subscription should pass, got %v", err)
	}
}

// atomicSlotRepo 用互斥锁模拟迁移 000027 的部分唯一索引语义：
// 同一订阅下，一个"活跃"实例保存成功时若已有其他活跃实例 → 返回 gorm.ErrDuplicatedKey。
// 用于并发 race 测试（真实 DB 的原子性由该唯一索引提供）。
type atomicSlotRepo struct {
	mu     sync.Mutex
	active map[string]string // subscriptionID -> 占用实例 ID
	insts  map[string]*entity.GameInstance
}

func newAtomicSlotRepo(insts ...*entity.GameInstance) *atomicSlotRepo {
	r := &atomicSlotRepo{active: map[string]string{}, insts: map[string]*entity.GameInstance{}}
	for _, i := range insts {
		r.insts[i.ID] = i
	}
	return r
}

func (r *atomicSlotRepo) Save(_ context.Context, inst *entity.GameInstance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insts[inst.ID] = inst
	if inst.SubscriptionID != nil {
		sub := *inst.SubscriptionID
		if inst.Status.IsActive() {
			if cur, ok := r.active[sub]; ok && cur != inst.ID {
				return gorm.ErrDuplicatedKey // 模拟唯一索引冲突
			}
			r.active[sub] = inst.ID
		} else {
			delete(r.active, sub)
		}
	}
	return nil
}

func (r *atomicSlotRepo) GetByID(_ context.Context, id string) (*entity.GameInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i, ok := r.insts[id]; ok {
		return i, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (r *atomicSlotRepo) UpdateStatus(ctx context.Context, inst *entity.GameInstance) error {
	return r.Save(ctx, inst)
}
func (r *atomicSlotRepo) ListByStatuses(ctx context.Context, statuses ...entity.InstanceStatus) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (r *atomicSlotRepo) ListAll(ctx context.Context) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (r *atomicSlotRepo) ListByGame(ctx context.Context, gameID string) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (r *atomicSlotRepo) ListActiveBySubscription(_ context.Context, sub string) ([]*entity.GameInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.active[sub]; ok {
		return []*entity.GameInstance{{ID: id, SubscriptionID: &sub}}, nil
	}
	return nil, nil
}
func (r *atomicSlotRepo) ListBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error) {
	return nil, nil
}
func (r *atomicSlotRepo) Delete(ctx context.Context, id string) error {
	return nil
}

// TestConcurrentStart_SameSubscription_ExactlyOneWins 并发 race（M11）：
// 同一订阅的两个实例同时 start，应用层先查后写存在竞态窗口，
// 兜底靠 DB 部分唯一索引（此处以 atomicSlotRepo 模拟）——
// 断言：恰好一个成功，另一个得到 ErrSubscriptionConflict。
func TestConcurrentStart_SameSubscription_ExactlyOneWins(t *testing.T) {
	a := stoppedInstance("inst-a", "sub-1")
	b := stoppedInstance("inst-b", "sub-1")
	repo := newAtomicSlotRepo(a, b)
	uc := newSlotUseCase(repo)

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, id := range []string{"inst-a", "inst-b"} {
		wg.Add(1)
		go func(idx int, instID string) {
			defer wg.Done()
			results[idx] = uc.StartGameInstance(context.Background(), instID)
		}(i, id)
	}
	wg.Wait()

	success, conflict := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrSubscriptionConflict):
			conflict++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("expected exactly 1 success + 1 conflict, got success=%d conflict=%d", success, conflict)
	}
}

// TestConcurrentStart_DifferentSubscriptions_BothSucceed 并发 start 不同订阅 → 互不影响
func TestConcurrentStart_DifferentSubscriptions_BothSucceed(t *testing.T) {
	a := stoppedInstance("inst-a", "sub-1")
	b := stoppedInstance("inst-b", "sub-2")
	repo := newAtomicSlotRepo(a, b)
	uc := newSlotUseCase(repo)

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, id := range []string{"inst-a", "inst-b"} {
		wg.Add(1)
		go func(idx int, instID string) {
			defer wg.Done()
			results[idx] = uc.StartGameInstance(context.Background(), instID)
		}(i, id)
	}
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Fatalf("start %d should succeed, got %v", i, err)
		}
	}
}

// ------------------------- 停止失败 vs 开启失败（可靠性增强） -------------------------

// failedStopInstance：停止/清理阶段失败 → Failed 且保留 node_agent 绑定（容器可能残留）
func failedStopInstance(id, sub string) *entity.GameInstance {
	agentID := "node-agent-1"
	return &entity.GameInstance{ID: id, GameID: "343050", Status: entity.Failed,
		NodeAgentID: &agentID, SubscriptionID: strPtr(sub)}
}

// failedStartInstance：启动/调度阶段失败 → Failed 且无绑定（无容器残留）
func failedStartInstance(id, sub string) *entity.GameInstance {
	return &entity.GameInstance{ID: id, GameID: "343050", Status: entity.Failed, SubscriptionID: strPtr(sub)}
}

// TestStartGameInstance_StopFailureRejected 停止失败（容器可能残留）→ 拒绝 start，
// 引导先停止清理；否则 start 会以同名 create_container 失败。
func TestStartGameInstance_StopFailureRejected(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return failedStopInstance("inst-a", "sub-1"), nil
		},
		listActive: func(ctx context.Context, sub string) ([]*entity.GameInstance, error) { return nil, nil },
	}
	uc := newSlotUseCase(repo)
	err := uc.StartGameInstance(context.Background(), "inst-a")
	if !errors.Is(err, ErrStopFailure) {
		t.Fatalf("expected ErrStopFailure, got %v", err)
	}
	if repo.saveCount != 0 {
		t.Fatalf("stop-failure start must not persist, saveCount=%d", repo.saveCount)
	}
}

// TestStartGameInstance_StartFailureAllowed 启动失败（无残留容器）→ 允许重新 start
func TestStartGameInstance_StartFailureAllowed(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return failedStartInstance("inst-a", "sub-1"), nil
		},
		listActive: func(ctx context.Context, sub string) ([]*entity.GameInstance, error) { return nil, nil },
	}
	uc := newSlotUseCase(repo)
	if err := uc.StartGameInstance(context.Background(), "inst-a"); err != nil {
		t.Fatalf("start-failure instance should be re-startable, got %v", err)
	}
}

// TestStopGameInstance_StopFailureRetriesStop 停止失败 → 允许重试停止（Failed→Stopping）
func TestStopGameInstance_StopFailureRetriesStop(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return failedStopInstance("inst-a", "sub-1"), nil
		},
	}
	uc := newSlotUseCase(repo)
	if err := uc.StopGameInstance(context.Background(), "inst-a"); err != nil {
		t.Fatalf("stop-failure should allow retry stop, got %v", err)
	}
	select {
	case inst := <-uc.ReconcileDispatcher.queue:
		if inst.Status != entity.StatusStopping {
			t.Fatalf("dispatched instance should be stopping, got %s", inst.Status)
		}
	default:
		t.Fatal("stop-failure retry should be dispatched")
	}
}

// TestStopGameInstance_StartFailureRejected 启动失败（无绑定）→ 无容器可停，拒绝 stop
func TestStopGameInstance_StartFailureRejected(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return failedStartInstance("inst-a", "sub-1"), nil
		},
	}
	uc := newSlotUseCase(repo)
	if err := uc.StopGameInstance(context.Background(), "inst-a"); err == nil {
		t.Fatal("start-failure instance should not be stoppable")
	}
}

// TestStopGameInstance_RunningAllowed Running 正常停止
func TestStopGameInstance_RunningAllowed(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return &entity.GameInstance{ID: "inst-a", GameID: "343050", Status: entity.StatusRunning}, nil
		},
	}
	uc := newSlotUseCase(repo)
	if err := uc.StopGameInstance(context.Background(), "inst-a"); err != nil {
		t.Fatalf("running instance should be stoppable, got %v", err)
	}
}

// TestRetryGameInstance_StopFailureRejected 停止失败不应走 retry（= 重新 start 撞容器）
func TestRetryGameInstance_StopFailureRejected(t *testing.T) {
	repo := &slotMockInstanceRepo{
		getByIDFunc: func(ctx context.Context, id string) (*entity.GameInstance, error) {
			return failedStopInstance("inst-a", "sub-1"), nil
		},
		listActive: func(ctx context.Context, sub string) ([]*entity.GameInstance, error) { return nil, nil },
	}
	uc := newSlotUseCase(repo)
	err := uc.RetryGameInstance(context.Background(), "inst-a")
	if !errors.Is(err, ErrStopFailure) {
		t.Fatalf("expected ErrStopFailure, got %v", err)
	}
}
