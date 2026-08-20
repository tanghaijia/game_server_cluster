package biz

import (
	"context"
	"testing"
	"time"

	"controller-go/internal/client/nodeagent"
	"controller-go/internal/entity"
	"controller-go/internal/repository"

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

var _ repository.NodeRepository = (*mockNodeRepo)(nil)

type mockReservationRepo struct{}

func (m *mockReservationRepo) TryReserve(ctx context.Context, req repository.ReserveTxRequest) error { return nil }
func (m *mockReservationRepo) Release(ctx context.Context, nodeID string, req entity.ResourceRequest) error {
	return nil
}

var _ repository.ReservationRepository = (*mockReservationRepo)(nil)

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
		nodeagent.NewClientRegistry(),
		nil,
		&mockGameRepo{},
		&mockGameContainerConfigRepo{},
		*mapper,
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
