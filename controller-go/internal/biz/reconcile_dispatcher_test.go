package biz

import (
	"context"
	"controller-go/internal/client/nodeagent"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	"testing"
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

// Ensure mockInstanceRepo implements repository.GameInstanceRepository
var _ repository.GameInstanceRepository = (*mockInstanceRepo)(nil)

type mockNodeAgentRepo struct{}

func (m *mockNodeAgentRepo) Save(ctx context.Context, agent *entity.NodeAgent) error { return nil }
func (m *mockNodeAgentRepo) GetByID(ctx context.Context, id string) (*entity.NodeAgent, error) {
	return &entity.NodeAgent{ID: id, NodeId: "1", Port: 9090}, nil
}

var _ repository.NodeAgentRepository = (*mockNodeAgentRepo)(nil)

type mockNodeRepo struct{}

func (m *mockNodeRepo) Save(node *entity.Node) error { return nil }
func (m *mockNodeRepo) GetByID(id string) (*entity.Node, error) {
	return &entity.Node{Id: 1, Ip: "127.0.0.1"}, nil
}

var _ repository.NodeRepository = (*mockNodeRepo)(nil)

type mockScheduler struct{}

func (m *mockScheduler) Schedule(gameInstance *entity.GameInstance) (string, error) {
	return "node-agent-1", nil
}

/**
* 测试ReconcileDispatcher的Dispatch和Process功能
**/
func TestReconcileDispatcher_DispatchAndProcess(t *testing.T) {
	repo := &mockInstanceRepo{
		saveFunc: func(ctx context.Context, inst *entity.GameInstance) error {
			return nil
		},
	}

	rd := NewReconcileDispatcher(repo, &mockNodeAgentRepo{}, &mockNodeRepo{}, &mockScheduler{}, nodeagent.NewClientRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inst := &entity.GameInstance{ID: "inst-1", Status: entity.StatusPending}
	rd.RequestDispatch(ctx, inst)

	err := rd.NextDispatch(ctx)
	if err != nil {
		t.Errorf("处理实例时出错: %v", err)
	}
	if inst.Status != entity.StatusScheduling {
		t.Errorf("实例状态未正确推进, 期望: %v, 实际: %v", entity.StatusScheduling, inst.Status)
	}

	cancel()
}
