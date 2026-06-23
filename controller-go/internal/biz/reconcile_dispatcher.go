package biz

import (
	"context"
	"controller-go/internal/entity"
	"errors"
	"fmt"
)

type ReconcileDispatcher struct {
	queue        chan *entity.GameInstance
	instanceRepo GameInstanceRepository
}

func NewReconcileDispatcher(instanceRepo GameInstanceRepository) *ReconcileDispatcher {
	return &ReconcileDispatcher{
		queue:        make(chan *entity.GameInstance, 100),
		instanceRepo: instanceRepo,
	}
}

/**
* 请求对一个GameInstance进行派遣
**/
func (d *ReconcileDispatcher) RequestDispatch(ctx context.Context, instance *entity.GameInstance) error {
	if instance == nil {
		return errors.New("instance cannot be nil")
	}

	if instance.Status == entity.StatusPending ||
		instance.Status == entity.StatusScheduling ||
		instance.Status == entity.StatusPreparingBuild ||
		instance.Status == entity.StatusRestoringSnapshot ||
		instance.Status == entity.StatusStopping {
		d.queue <- instance
		return nil
	}

	return errors.New("instance is not in a dispatchable state")
}

/**
* 派遣下一个需要派遣的GameInstance, 若队列中为空则会阻塞
**/
func (d *ReconcileDispatcher) NextDispatch(ctx context.Context) error {
	instance := <-d.queue

	status, err := instance.Advance(ctx) // 这里调用Advance方法来推进状态
	if err != nil {
		fmt.Printf("Error advancing instance %s: %v\n", instance.ID, err)
		return err
	} else {
		fmt.Printf("Instance %s advanced to status %d\n", instance.ID, status)
	}

	if status != entity.StatusRunning && status != entity.StatusStopped {
		// 处理其他状态
		d.queue <- instance // 将实例重新放回队列中以继续处理
	}

	err = d.instanceRepo.UpdateStatus(ctx, instance) // 假设这里需要上下文
	if err != nil {
		fmt.Printf("Error updating instance %s status: %v\n", instance.ID, err)
		return err
	}

	return nil
}

/**
* 开启一个goroutine来处理派遣队列中的GameInstance
**/
func (d *ReconcileDispatcher) Start(ctx context.Context) {
	go func() {
		for {
			// 处理派遣逻辑
			d.NextDispatch(ctx)
		}
	}()
}
