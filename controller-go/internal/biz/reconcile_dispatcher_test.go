package biz

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"controller-go/internal/client/nodeagent"
	"controller-go/internal/entity"
	"controller-go/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type mockInstanceRepo struct {
	saveFunc    func(ctx context.Context, inst *entity.GameInstance) error
	getByIDFunc func(ctx context.Context, id string) (*entity.GameInstance, error)
}

func (m *mockInstanceRepo) Save(ctx context.Context, inst *entity.GameInstance) error {
	return m.saveFunc(ctx, inst)
}

func (m *mockInstanceRepo) GetByID(ctx context.Context, id string) (*entity.GameInstance, error) {
	return m.getByIDFunc(ctx, id)
}

func (m *mockInstanceRepo) UpdateStatus(ctx context.Context, inst *entity.GameInstance) error {
	return m.saveFunc(ctx, inst)
}

func (m *mockInstanceRepo) ListByStatuses(ctx context.Context, statuses ...entity.InstanceStatus) ([]*entity.GameInstance, error) {
	return nil, nil
}

func (m *mockInstanceRepo) ListAll(ctx context.Context) ([]*entity.GameInstance, error) {
	return nil, nil
}

func (m *mockInstanceRepo) ListByGame(ctx context.Context, gameID string) ([]*entity.GameInstance, error) {
	return nil, nil
}

func (m *mockInstanceRepo) ListActiveBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error) {
	return nil, nil
}

func (m *mockInstanceRepo) ListBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error) {
	return nil, nil
}

func (m *mockInstanceRepo) Delete(ctx context.Context, id string) error {
	return nil
}

// Ensure mockInstanceRepo implements repository.GameInstanceRepository
var _ repository.GameInstanceRepository = (*mockInstanceRepo)(nil)

type mockNodeAgentRepo struct{}

func (m *mockNodeAgentRepo) Save(ctx context.Context, agent *entity.NodeAgent) error { return nil }
func (m *mockNodeAgentRepo) GetByID(ctx context.Context, id string) (*entity.NodeAgent, error) {
	return &entity.NodeAgent{ID: id, NodeId: "1", Port: 9090}, nil
}
func (m *mockNodeAgentRepo) ListEnabledIDs(ctx context.Context) ([]string, error) {
	return []string{"node-agent-1"}, nil
}

func (m *mockNodeAgentRepo) ListAll(ctx context.Context) ([]*entity.NodeAgent, error) {
	return nil, nil
}

func (m *mockNodeAgentRepo) UpdateHealth(ctx context.Context, agentID string, alive bool, at time.Time) error {
	return nil
}

func (m *mockNodeAgentRepo) UpdateHealthStatus(ctx context.Context, agentID string, status entity.NodeAgentHealthStatus) error {
	return nil
}

func (m *mockNodeAgentRepo) UpdateAgentVersion(ctx context.Context, agentID, version string) error {
	return nil
}

func (m *mockNodeAgentRepo) UpdateUpdateState(ctx context.Context, agentID, state, targetVersion, errMsg string) error {
	return nil
}

var _ repository.NodeAgentRepository = (*mockNodeAgentRepo)(nil)

type mockNodeRepo struct{}

func (m *mockNodeRepo) Save(node *entity.Node) error { return nil }
func (m *mockNodeRepo) GetByID(id string) (*entity.Node, error) {
	return &entity.Node{Id: 1, Ip: "127.0.0.1"}, nil
}

func (m *mockNodeRepo) ListAll(ctx context.Context) ([]*entity.Node, error) {
	return nil, nil
}

func (m *mockNodeRepo) UpdateDynamicUsage(ctx context.Context, nodeID string, u entity.NodeDynamicUsage, reportedAt time.Time) error {
	return nil
}

func (m *mockNodeRepo) UpdatePressureStatus(ctx context.Context, nodeID string, status entity.NodePressureStatus) error {
	return nil
}

func (m *mockNodeRepo) Delete(ctx context.Context, id string) error { return nil }

var _ repository.NodeRepository = (*mockNodeRepo)(nil)

type mockReservationRepo struct {
	releaseCount int
}

func (m *mockReservationRepo) TryReserve(ctx context.Context, req repository.ReserveTxRequest) error { return nil }
func (m *mockReservationRepo) Release(ctx context.Context, nodeID string, req entity.ResourceRequest) error {
	m.releaseCount++
	return nil
}

var _ repository.ReservationRepository = (*mockReservationRepo)(nil)

// TestFailedInstance_NoReleaseOnScheduleFailure 调度失败不应改变预留的回归测试：
// 调度阶段失败（Scheduling→Failed）本次未扣减预留——即使实例残留上次的绑定，
// FailedInstance 也不得释放（否则"调度失败但节点预留变化"）；并清空绑定。
func TestFailedInstance_NoReleaseOnScheduleFailure(t *testing.T) {
	repo := &mockInstanceRepo{saveFunc: func(ctx context.Context, inst *entity.GameInstance) error { return nil }}
	resv := &mockReservationRepo{}
	rd := NewReconcileDispatcher(
		repo, &mockNodeAgentRepo{}, &mockNodeRepo{}, &mockScheduler{}, resv,
		NewQueueManager(&mockQueueRepo{}, 15*time.Second, 5*time.Minute, 30*time.Minute),
		NewSchedulerEventBus(100, nil),
		nodeagent.NewClientRegistry(), nil, &mockGameRepo{}, &mockGameContainerConfigRepo{},
		GameContainerPortMapper{},
		nil, // platformConfigRepo（本测试不涉及平台配置合并）
		nil, // credentialUC（M8：本测试不涉及凭证分配）
		30*time.Second, // B-12：RPC 超时
		3*time.Minute,  // B-14：节点失联 fencing 阈值
	)

	// 调度阶段失败：status=Scheduling + 残留上次绑定 → 不得释放
	agentID := "node-agent-1"
	inst := &entity.GameInstance{
		ID: "inst-sched-fail", Status: entity.StatusScheduling,
		NodeAgentID:  &agentID,
		ResourceReq:  &entity.ResourceRequest{CPUMilli: 1000, MemoryBytes: 1 << 30},
	}
	rd.FailedInstance(context.Background(), inst)
	if resv.releaseCount != 0 {
		t.Fatalf("调度阶段失败不应释放预留, releaseCount=%d", resv.releaseCount)
	}
	if inst.NodeAgentID != nil {
		t.Fatalf("失败后应清空 NodeAgentID")
	}

	// 阶段失败（PreparingBuild）：已成功绑定并扣减 → 应释放
	inst2 := &entity.GameInstance{
		ID: "inst-phase-fail", Status: entity.StatusPreparingBuild,
		NodeAgentID: &agentID,
		ResourceReq: &entity.ResourceRequest{CPUMilli: 1000, MemoryBytes: 1 << 30},
	}
	rd.FailedInstance(context.Background(), inst2)
	if resv.releaseCount != 1 {
		t.Fatalf("阶段失败应释放预留, releaseCount=%d", resv.releaseCount)
	}
}

// TestFailedInstance_KeepBindingOnStopFailure 停止阶段失败保留 node_agent 绑定的回归测试：
// StopInstance 失败后容器可能仍残留在 node_agent 上，清空绑定会导致"停止失败后重试停止"
// 无法定位节点；同时应清空 ResourceReq（预留已释放），避免重试停止成功进入清理阶段时二次释放。
func TestFailedInstance_KeepBindingOnStopFailure(t *testing.T) {
	repo := &mockInstanceRepo{saveFunc: func(ctx context.Context, inst *entity.GameInstance) error { return nil }}
	resv := &mockReservationRepo{}
	rd := NewReconcileDispatcher(
		repo, &mockNodeAgentRepo{}, &mockNodeRepo{}, &mockScheduler{}, resv,
		NewQueueManager(&mockQueueRepo{}, 15*time.Second, 5*time.Minute, 30*time.Minute),
		NewSchedulerEventBus(100, nil),
		nodeagent.NewClientRegistry(), nil, &mockGameRepo{}, &mockGameContainerConfigRepo{},
		GameContainerPortMapper{},
		nil, // platformConfigRepo
		nil, // credentialUC（M8：本测试不涉及凭证分配）
		30*time.Second, // B-12：RPC 超时
		3*time.Minute,  // B-14：节点失联 fencing 阈值
	)

	agentID := "node-agent-1"
	inst := &entity.GameInstance{
		ID: "inst-stop-fail", Status: entity.StatusStopping,
		NodeAgentID: &agentID,
		ResourceReq: &entity.ResourceRequest{CPUMilli: 1000, MemoryBytes: 1 << 30},
	}
	rd.FailedInstance(context.Background(), inst)
	if inst.Status != entity.Failed {
		t.Fatalf("状态应为 Failed, 实际: %v", inst.Status)
	}
	if inst.NodeAgentID == nil || *inst.NodeAgentID != agentID {
		t.Fatalf("停止阶段失败应保留 NodeAgentID 绑定（供停止重试定位节点）, 实际: %v", inst.NodeAgentID)
	}
	if inst.ResourceReq != nil {
		t.Fatalf("停止阶段失败应清空 ResourceReq（防清理阶段二次释放）, 实际: %v", inst.ResourceReq)
	}
	if resv.releaseCount != 1 {
		t.Fatalf("停止阶段失败应释放预留, releaseCount=%d", resv.releaseCount)
	}
}

// TestFailedInstance_ClearBindingOnStartFailure 启动阶段失败仍应清空绑定的回归测试：
// 仅停止/清理阶段保留绑定，其余阶段失败（如 Starting 启动失败）照旧清空 NodeAgentID。
func TestFailedInstance_ClearBindingOnStartFailure(t *testing.T) {
	repo := &mockInstanceRepo{saveFunc: func(ctx context.Context, inst *entity.GameInstance) error { return nil }}
	resv := &mockReservationRepo{}
	rd := NewReconcileDispatcher(
		repo, &mockNodeAgentRepo{}, &mockNodeRepo{}, &mockScheduler{}, resv,
		NewQueueManager(&mockQueueRepo{}, 15*time.Second, 5*time.Minute, 30*time.Minute),
		NewSchedulerEventBus(100, nil),
		nodeagent.NewClientRegistry(), nil, &mockGameRepo{}, &mockGameContainerConfigRepo{},
		GameContainerPortMapper{},
		nil, // platformConfigRepo
		nil, // credentialUC
		30*time.Second, // B-12
		3*time.Minute,  // B-14
	)

	agentID := "node-agent-1"
	inst := &entity.GameInstance{
		ID: "inst-start-fail", Status: entity.StatusStarting,
		NodeAgentID: &agentID,
		ResourceReq: &entity.ResourceRequest{CPUMilli: 1000, MemoryBytes: 1 << 30},
	}
	rd.FailedInstance(context.Background(), inst)
	if inst.NodeAgentID != nil {
		t.Fatalf("启动阶段失败应清空 NodeAgentID, 实际: %v", inst.NodeAgentID)
	}
}

type mockQueueRepo struct{}

func (m *mockQueueRepo) Enqueue(ctx context.Context, q *entity.SchedulingQueue) error { return nil }
func (m *mockQueueRepo) Dequeue(ctx context.Context, instanceID string) error         { return nil }
func (m *mockQueueRepo) Get(ctx context.Context, instanceID string) (*entity.SchedulingQueue, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *mockQueueRepo) UpdateWake(ctx context.Context, instanceID string, wakeAt time.Time, attempts int) error {
	return nil
}
func (m *mockQueueRepo) ListDue(ctx context.Context, now time.Time) ([]*entity.SchedulingQueue, error) {
	return nil, nil
}
func (m *mockQueueRepo) ListAll(ctx context.Context) ([]*entity.SchedulingQueue, error) {
	return nil, nil
}
func (m *mockQueueRepo) Count(ctx context.Context) (int64, error) { return 0, nil }

var _ repository.SchedulingQueueRepository = (*mockQueueRepo)(nil)

type mockScheduler struct {
	scheduleFunc func(ctx context.Context, inst *entity.GameInstance) (*ScheduleResult, error)
}

func (m *mockScheduler) Schedule(ctx context.Context, inst *entity.GameInstance) (*ScheduleResult, error) {
	if m.scheduleFunc != nil {
		return m.scheduleFunc(ctx, inst)
	}
	return &ScheduleResult{Outcome: OutcomeScheduled, NodeAgentID: "node-agent-1",
		ResourceReq: entity.ResourceRequest{CPUMilli: 1000, MemoryBytes: 1 << 30, DiskBytes: 10 << 30}}, nil
}

func (m *mockScheduler) CancelQueued(ctx context.Context, instanceID string) error { return nil }
func (m *mockScheduler) QueueStats() map[string]any                           { return nil }

type mockGameRepo struct{}

func (m *mockGameRepo) Save(ctx context.Context, game *entity.Game) error { return nil }
func (m *mockGameRepo) GetByID(ctx context.Context, id string) (*entity.Game, error) {
	return &entity.Game{ID: id, ContainerConfigID: "cfg-1"}, nil
}

func (m *mockGameRepo) ListAll(ctx context.Context) ([]*entity.Game, error) {
	return nil, nil
}

func (m *mockGameRepo) Delete(ctx context.Context, id string) error {
	return nil
}

var _ repository.GameRepository = (*mockGameRepo)(nil)

type mockGameContainerConfigRepo struct{}

func (m *mockGameContainerConfigRepo) Save(ctx context.Context, config *entity.GameContainerConfig) error {
	return nil
}
func (m *mockGameContainerConfigRepo) GetByID(ctx context.Context, id string) (*entity.GameContainerConfig, error) {
	return &entity.GameContainerConfig{
		ID:       id,
		PortMode: entity.PORT_MAPPING_MOD_NAT,
		PortExcerpt: []entity.GameContainerPortExcerpt{
			{Protocol: entity.TCP, BeginPort: 1000, ExcerptLength: 2},
		},
	}, nil
}

func (m *mockGameContainerConfigRepo) Delete(ctx context.Context, id string) error {
	return nil
}

func (m *mockGameContainerConfigRepo) ReplacePortExcerpts(ctx context.Context, configID string, excerpts []entity.GameContainerPortExcerpt) error {
	return nil
}

var _ repository.GameContainerConfigRepository = (*mockGameContainerConfigRepo)(nil)

type mockPortMappingRepo struct {
	mappings []*entity.ContainerPortMapping
}

func (m *mockPortMappingRepo) Save(ctx context.Context, mapping *entity.ContainerPortMapping) error {
	m.mappings = append(m.mappings, mapping)
	return nil
}
func (m *mockPortMappingRepo) GetByID(ctx context.Context, id string) (*entity.ContainerPortMapping, error) {
	return nil, nil
}
func (m *mockPortMappingRepo) DeleteById(ctx context.Context, id string) error { return nil }
func (m *mockPortMappingRepo) ListByInstanceId(ctx context.Context, instanceId string) ([]*entity.ContainerPortMapping, error) {
	var result []*entity.ContainerPortMapping
	for _, mp := range m.mappings {
		if mp.InstanceId == instanceId {
			result = append(result, mp)
		}
	}
	return result, nil
}
func (m *mockPortMappingRepo) ListByNodeAgentId(ctx context.Context, nodeAgentId string) ([]*entity.ContainerPortMapping, error) {
	var result []*entity.ContainerPortMapping
	for _, mp := range m.mappings {
		if mp.NodeAgentId == nodeAgentId {
			result = append(result, mp)
		}
	}
	return result, nil
}
func (m *mockPortMappingRepo) DeleteByInstanceId(ctx context.Context, instanceId string) error {
	var result []*entity.ContainerPortMapping
	for _, mp := range m.mappings {
		if mp.InstanceId != instanceId {
			result = append(result, mp)
		}
	}
	m.mappings = result
	return nil
}

var _ repository.ContainerPortMappingRepository = (*mockPortMappingRepo)(nil)

/**
* 测试ReconcileDispatcher的Dispatch和Process功能
**/
func TestReconcileDispatcher_DispatchAndProcess(t *testing.T) {
	repo := &mockInstanceRepo{
		saveFunc: func(ctx context.Context, inst *entity.GameInstance) error {
			return nil
		},
	}

	portMappingRepo := &mockPortMappingRepo{}
	mapper := NewGameContainerPortMapper(portMappingRepo, &mockGameContainerConfigRepo{})

	// 模拟 ResourceAwareScheduler：调度阶段完成端口分配（新架构下端口分配在 scheduler 内部完成）
	sch := &mockScheduler{
		scheduleFunc: func(ctx context.Context, inst *entity.GameInstance) (*ScheduleResult, error) {
			mappings, err := mapper.PlanPorts(ctx,
				&entity.NodeAgent{ID: "node-agent-1"},
				&entity.Game{ID: "343050", ContainerConfigID: "cfg-1"}, inst)
			if err != nil {
				return nil, err
			}
			for i := range mappings {
				portMappingRepo.mappings = append(portMappingRepo.mappings, &mappings[i])
			}
			return &ScheduleResult{Outcome: OutcomeScheduled, NodeAgentID: "node-agent-1",
				ResourceReq: entity.ResourceRequest{CPUMilli: 1000, MemoryBytes: 1 << 30, DiskBytes: 10 << 30}}, nil
		},
	}

	rd := NewReconcileDispatcher(
		repo,
		&mockNodeAgentRepo{},
		&mockNodeRepo{},
		sch,
		&mockReservationRepo{},
		NewQueueManager(&mockQueueRepo{}, 15*time.Second, 5*time.Minute, 30*time.Minute),
		NewSchedulerEventBus(100, nil),
		nodeagent.NewClientRegistry(),
		nil,
		&mockGameRepo{},
		&mockGameContainerConfigRepo{},
		*mapper,
		nil, // platformConfigRepo
		nil, // credentialUC（M8：本测试不涉及凭证分配）
		30*time.Second, // B-12：RPC 超时
		3*time.Minute,  // B-14：节点失联 fencing 阈值
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inst := &entity.GameInstance{ID: "inst-1", GameID: "343050", Status: entity.StatusPending}
	rd.RequestDispatch(ctx, inst)

	err := rd.NextDispatch(ctx)
	if err != nil {
		t.Errorf("处理实例时出错: %v", err)
	}
	if inst.Status != entity.StatusScheduling {
		t.Errorf("实例状态未正确推进, 期望: %v, 实际: %v", entity.StatusScheduling, inst.Status)
	}

	// 第二次派遣处理调度阶段：分配 node_agent 并为实例分配端口
	err = rd.NextDispatch(ctx)
	if err != nil {
		t.Errorf("处理实例时出错: %v", err)
	}
	if inst.Status != entity.StatusPreparingBuild {
		t.Errorf("实例状态未正确推进, 期望: %v, 实际: %v", entity.StatusPreparingBuild, inst.Status)
	}
	if inst.NodeAgentID == nil || *inst.NodeAgentID != "node-agent-1" {
		t.Errorf("实例未正确分配 node_agent, 实际: %v", inst.NodeAgentID)
	}
	// 端口片段长度为 2，应分配 2 条端口映射
	if len(portMappingRepo.mappings) != 2 {
		t.Errorf("调度阶段应分配 2 条端口映射, 实际: %d", len(portMappingRepo.mappings))
	}

	cancel()
}

// ------------------------- P0 稳定性回归测试（B-12/13/14） -------------------------

// B-12：RPC 瞬态错误可重试分类（超时/Unavailable 不误杀实例，非瞬态照旧失败）
func TestIsRetryableRPCErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain", errors.New("boom"), false},
		{"deadline", context.DeadlineExceeded, true},
		{"grpc_unavailable", status.Error(codes.Unavailable, "node down"), true},
		{"grpc_deadline", status.Error(codes.DeadlineExceeded, "too slow"), true},
		{"grpc_aborted", status.Error(codes.Aborted, "retry"), true},
		{"grpc_notfound", status.Error(codes.NotFound, "op gone"), false},
		{"wrapped_deadline", fmt.Errorf("wrap: %w", context.DeadlineExceeded), true},
	}
	for _, c := range cases {
		if got := isRetryableRPCErr(c.err); got != c.want {
			t.Errorf("%s: isRetryableRPCErr(%v) = %v, want %v", c.name, c.err, got, c.want)
		}
	}
}

// B-13：队列满时 RequestDispatch 非阻塞（防消费端自入队自死锁），且溢出实例不丢失
func TestRequestDispatch_QueueFull_NonBlocking(t *testing.T) {
	rd := NewReconcileDispatcher(
		&mockInstanceRepo{saveFunc: func(ctx context.Context, inst *entity.GameInstance) error { return nil }},
		&mockNodeAgentRepo{}, &mockNodeRepo{}, &mockScheduler{}, &mockReservationRepo{},
		NewQueueManager(&mockQueueRepo{}, 15*time.Second, 5*time.Minute, 30*time.Minute),
		NewSchedulerEventBus(100, nil),
		nodeagent.NewClientRegistry(), nil, &mockGameRepo{}, &mockGameContainerConfigRepo{},
		GameContainerPortMapper{},
		nil, // platformConfigRepo
		nil, // credentialUC
		30*time.Second, // B-12
		3*time.Minute,  // B-14
	)
	ctx := context.Background()

	// 填满内部 channel（容量 100）
	for i := 0; i < 100; i++ {
		inst := &entity.GameInstance{ID: fmt.Sprintf("inst-%03d", i), Status: entity.StatusPending}
		if err := rd.RequestDispatch(ctx, inst); err != nil {
			t.Fatalf("填充队列失败: %v", err)
		}
	}

	// 第 101 个：旧实现会阻塞（自死锁隐患），新实现必须立即返回
	done := make(chan error, 1)
	go func() {
		inst := &entity.GameInstance{ID: "inst-overflow", Status: entity.StatusPending}
		done <- rd.RequestDispatch(ctx, inst)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("满队列入队不应报错: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("B-13: 队列满时 RequestDispatch 必须非阻塞，疑似自死锁")
	}

	// 消费端优先取 overflow：第一个被消费的应是溢出实例，其余 100 个来自 channel
	first := rd.nextDispatchInstance(ctx)
	if first == nil || first.ID != "inst-overflow" {
		t.Fatalf("消费端应优先消费溢出实例, got: %v", first)
	}
	for i := 0; i < 100; i++ {
		if inst := rd.nextDispatchInstance(ctx); inst == nil {
			t.Fatalf("drain 第 %d 个实例意外为 nil", i)
		}
	}
}

// B-14：节点失联计时——首次记账不触发，持续超阈值才触发；恢复后清零重计
func TestFenceOfflineTiming(t *testing.T) {
	rd := NewReconcileDispatcher(
		&mockInstanceRepo{saveFunc: func(ctx context.Context, inst *entity.GameInstance) error { return nil }},
		&mockNodeAgentRepo{}, &mockNodeRepo{}, &mockScheduler{}, &mockReservationRepo{},
		NewQueueManager(&mockQueueRepo{}, 15*time.Second, 5*time.Minute, 30*time.Minute),
		NewSchedulerEventBus(100, nil),
		nodeagent.NewClientRegistry(), nil, &mockGameRepo{}, &mockGameContainerConfigRepo{},
		GameContainerPortMapper{},
		nil, // platformConfigRepo
		nil, // credentialUC
		30*time.Second,
		50*time.Millisecond, // 缩短阈值便于测试
	)

	if rd.markOfflineAndShouldFence("inst-1") {
		t.Fatal("首次观测失联不应立即触发 fencing")
	}
	time.Sleep(60 * time.Millisecond)
	if !rd.markOfflineAndShouldFence("inst-1") {
		t.Fatal("持续失联超过阈值应触发 fencing")
	}

	// 恢复可达 → 清零 → 重新计时
	rd.clearOffline("inst-1")
	if rd.markOfflineAndShouldFence("inst-1") {
		t.Fatal("clearOffline 后应重新计时，不立即触发")
	}
}
