package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// mockQueueRepoForTest 可注入行为的队列仓库 mock
type mockQueueRepoForTest struct {
	getFunc   func(ctx context.Context, id string) (*entity.SchedulingQueue, error)
	updateWakeFunc func(ctx context.Context, id string, wakeAt time.Time, attempts int) error
	enqueueFunc func(ctx context.Context, q *entity.SchedulingQueue) error
	dequeueFunc func(ctx context.Context, id string) error
	countFunc func(ctx context.Context) (int64, error)
}

func (m *mockQueueRepoForTest) Enqueue(ctx context.Context, q *entity.SchedulingQueue) error {
	if m.enqueueFunc != nil {
		return m.enqueueFunc(ctx, q)
	}
	return nil
}
func (m *mockQueueRepoForTest) Dequeue(ctx context.Context, instanceID string) error {
	if m.dequeueFunc != nil {
		return m.dequeueFunc(ctx, instanceID)
	}
	return nil
}
func (m *mockQueueRepoForTest) Get(ctx context.Context, instanceID string) (*entity.SchedulingQueue, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, instanceID)
	}
	return nil, errors.New("not found")
}
func (m *mockQueueRepoForTest) UpdateWake(ctx context.Context, instanceID string, wakeAt time.Time, attempts int) error {
	if m.updateWakeFunc != nil {
		return m.updateWakeFunc(ctx, instanceID, wakeAt, attempts)
	}
	return nil
}
func (m *mockQueueRepoForTest) ListDue(ctx context.Context, now time.Time) ([]*entity.SchedulingQueue, error) {
	return nil, nil
}
func (m *mockQueueRepoForTest) ListAll(ctx context.Context) ([]*entity.SchedulingQueue, error) {
	return nil, nil
}
func (m *mockQueueRepoForTest) Count(ctx context.Context) (int64, error) {
	if m.countFunc != nil {
		return m.countFunc(ctx)
	}
	return 0, nil
}

var _ repository.SchedulingQueueRepository = (*mockQueueRepoForTest)(nil)

// TestQueueBackoff 退避序列（D9）：15s,30s,1m,2m,4m,5m(上限)
func TestQueueBackoff(t *testing.T) {
	qm := NewQueueManager(nil, 15*time.Second, 5*time.Minute, 30*time.Minute)
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 15 * time.Second},
		{2, 30 * time.Second},
		{3, 1 * time.Minute},
		{4, 2 * time.Minute},
		{5, 4 * time.Minute},
		{6, 5 * time.Minute}, // 上限
		{10, 5 * time.Minute},
	}
	for _, c := range cases {
		if got := qm.backoff(c.attempts); got != c.want {
			t.Errorf("backoff(%d) = %v, 期望 %v", c.attempts, got, c.want)
		}
	}
}

// TestQueueOnStillQueued 重试仍不足：attempts 递增 + 退避更新
func TestQueueOnStillQueued(t *testing.T) {
	var gotWakeAt time.Time
	var gotAttempts int
	repo := &mockQueueRepoForTest{
		getFunc: func(ctx context.Context, id string) (*entity.SchedulingQueue, error) {
			return &entity.SchedulingQueue{
				InstanceID: id, Attempts: 1, Priority: 100,
				QueuedAt: time.Now(), TimeoutAt: time.Now().Add(30 * time.Minute),
			}, nil
		},
		updateWakeFunc: func(ctx context.Context, id string, wakeAt time.Time, attempts int) error {
			gotWakeAt, gotAttempts = wakeAt, attempts
			return nil
		},
	}
	qm := NewQueueManager(repo, 15*time.Second, 5*time.Minute, 30*time.Minute)

	timeout, err := qm.OnStillQueued(context.Background(), "inst-1", false)
	if err != nil {
		t.Fatalf("OnStillQueued err: %v", err)
	}
	if timeout {
		t.Fatal("不应超时")
	}
	if gotAttempts != 2 {
		t.Errorf("attempts = %d, 期望 2", gotAttempts)
	}
	want := time.Now().Add(30 * time.Second)
	if gotWakeAt.Sub(want) > time.Second {
		t.Errorf("wake_at 应为 ~30s 退避（attempts=2），实际 %v", gotWakeAt)
	}
}

// TestQueueTimeout 排队超时（S16）：超过 timeout_at → OnStillQueued 返回超时
func TestQueueTimeout(t *testing.T) {
	repo := &mockQueueRepoForTest{
		getFunc: func(ctx context.Context, id string) (*entity.SchedulingQueue, error) {
			return &entity.SchedulingQueue{
				InstanceID: id, Attempts: 5, Priority: 100,
				QueuedAt: time.Now().Add(-31 * time.Minute),
				TimeoutAt: time.Now().Add(-1 * time.Minute), // 已超时
			}, nil
		},
	}
	qm := NewQueueManager(repo, 15*time.Second, 5*time.Minute, 30*time.Minute)
	timeout, err := qm.OnStillQueued(context.Background(), "inst-1", false)
	if err != nil {
		t.Fatalf("OnStillQueued err: %v", err)
	}
	if !timeout {
		t.Fatal("应判定超时")
	}
}

// TestQueueEventWakeResetsBackoff 事件唤醒重置退避（S14）
func TestQueueEventWakeResetsBackoff(t *testing.T) {
	var gotAttempts int
	repo := &mockQueueRepoForTest{
		getFunc: func(ctx context.Context, id string) (*entity.SchedulingQueue, error) {
			return &entity.SchedulingQueue{
				InstanceID: id, Attempts: 10, Priority: 100,
				QueuedAt: time.Now(), TimeoutAt: time.Now().Add(30 * time.Minute),
			}, nil
		},
		updateWakeFunc: func(ctx context.Context, id string, wakeAt time.Time, attempts int) error {
			gotAttempts = attempts
			return nil
		},
	}
	qm := NewQueueManager(repo, 15*time.Second, 5*time.Minute, 30*time.Minute)
	timeout, err := qm.OnStillQueued(context.Background(), "inst-1", true)
	if err != nil || timeout {
		t.Fatalf("OnStillQueued err=%v timeout=%v", err, timeout)
	}
	if gotAttempts != 1 {
		t.Errorf("事件唤醒后 attempts 应为 1（重置退避），实际 %d", gotAttempts)
	}
}
