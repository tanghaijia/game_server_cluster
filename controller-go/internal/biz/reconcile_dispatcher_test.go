package biz

import (
	"context"
	"controller-go/internal/entity"
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

/**
* 测试ReconcileDispatcher的Dispatch和Process功能
**/
func TestReconcileDispatcher_DispatchAndProcess(t *testing.T) {
	repo := &mockInstanceRepo{
		saveFunc: func(ctx context.Context, inst *entity.GameInstance) error {
			return nil
		},
	}

	rd := NewReconcileDispatcher(repo)
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
