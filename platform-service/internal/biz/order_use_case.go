package biz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"platform-service/internal/client/controller"
	"platform-service/internal/entity"
	"platform-service/internal/repository"
)

// OrderUseCase 订单业务逻辑
type OrderUseCase struct {
	repo       repository.OrderRepository
	controller *controller.Client
}

func NewOrderUseCase(repo repository.OrderRepository, controllerClient *controller.Client) *OrderUseCase {
	return &OrderUseCase{repo: repo, controller: controllerClient}
}

// CreateOrder 创建订单（初始状态 created）
func (uc *OrderUseCase) CreateOrder(ctx context.Context, userID, gameID string, amount int64) (*entity.Order, error) {
	if userID == "" {
		return nil, errors.New("user_id is required")
	}
	if gameID == "" {
		return nil, errors.New("game_id is required")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be positive")
	}

	now := time.Now()
	order := &entity.Order{
		ID:         newEntityID("order"),
		UserID:     userID,
		GameID:     gameID,
		Amount:     amount,
		Status:     entity.OrderStatusCreated,
		CreateTime: now,
		UpdateTime: now,
	}
	if err := uc.repo.Save(ctx, order); err != nil {
		return nil, err
	}
	return order, nil
}

// GetOrder 按 id 查询订单
func (uc *OrderUseCase) GetOrder(ctx context.Context, id string) (*entity.Order, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	return uc.repo.GetByID(ctx, id)
}

// FileSession 获取实例的文件会话（controller 签发短效 JWT，供浏览器直连 node_agent）
func (uc *OrderUseCase) FileSession(ctx context.Context, instanceID string) (*controller.FileSession, error) {
	return uc.controller.CreateFileSession(ctx, instanceID)
}

// ListOrders 列出订单；userID 非空时只列该用户的订单
func (uc *OrderUseCase) ListOrders(ctx context.Context, userID string) ([]*entity.Order, error) {
	if userID != "" {
		return uc.repo.ListByUser(ctx, userID)
	}
	return uc.repo.ListAll(ctx)
}

// PayOrder 支付订单（占位：无真实支付渠道，直接标记已支付）并编排实例：
// 调用 controller 创建 game_instance 并启动，回填 order.InstanceID。
func (uc *OrderUseCase) PayOrder(ctx context.Context, orderID string) (*entity.Order, error) {
	order, err := uc.repo.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != entity.OrderStatusCreated {
		return nil, fmt.Errorf("order is in status %d, only created can be paid", order.Status)
	}
	return uc.provisionInstance(ctx, order, entity.OrderStatusPaid)
}

// ProvisionOrder 管理员免支付直接开服：created → provisioned + 实例。
// 与支付解耦（支付是收钱动作，开服是编排动作，见前端设计讨论）。
func (uc *OrderUseCase) ProvisionOrder(ctx context.Context, orderID string) (*entity.Order, error) {
	order, err := uc.repo.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != entity.OrderStatusCreated {
		return nil, fmt.Errorf("order is in status %d, only created can be provisioned", order.Status)
	}
	return uc.provisionInstance(ctx, order, entity.OrderStatusProvisioned)
}

// provisionInstance 共用"开单"逻辑：调 controller 创建实例（stopped，不启动），
// 回填 instance_id，订单进入 finalStatus 并落库。
// 启动由用户/管理员通过 StartInstance 显式触发（创建与开服解耦）。
func (uc *OrderUseCase) provisionInstance(ctx context.Context, order *entity.Order, finalStatus entity.OrderStatus) (*entity.Order, error) {
	inst, err := uc.controller.CreateGameInstance(ctx, order.GameID, "")
	if err != nil {
		return nil, fmt.Errorf("create game instance via controller: %w", err)
	}

	order.InstanceID = inst.ID
	order.Status = finalStatus
	order.UpdateTime = time.Now()
	if err := uc.repo.Save(ctx, order); err != nil {
		return nil, err
	}
	return order, nil
}

// StartInstance 启动订单关联的实例（仅 paid / provisioned 且已有实例可启动）
func (uc *OrderUseCase) StartInstance(ctx context.Context, orderID string) (*entity.Order, error) {
	order, err := uc.repo.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.InstanceID == "" {
		return nil, errors.New("order has no instance, pay or provision first")
	}
	if err := uc.controller.StartGameInstance(ctx, order.InstanceID); err != nil {
		return nil, fmt.Errorf("start game instance via controller: %w", err)
	}
	return order, nil
}

// StopInstance 停止订单关联的实例
func (uc *OrderUseCase) StopInstance(ctx context.Context, orderID string) (*entity.Order, error) {
	order, err := uc.repo.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.InstanceID == "" {
		return nil, errors.New("order has no instance")
	}
	if err := uc.controller.StopGameInstance(ctx, order.InstanceID); err != nil {
		return nil, fmt.Errorf("stop game instance via controller: %w", err)
	}
	return order, nil
}

// UserInstance 用户侧实例视图（订单 + controller 实例状态）
type UserInstance struct {
	OrderID    string `json:"order_id"`
	InstanceID string `json:"instance_id"`
	GameID     string `json:"game_id"`
	Status     string `json:"status"`
	NodeAgent  string `json:"node_agent,omitempty"`
}

// ListInstances 返回订单关联的实例列表。userID 为空表示全部（管理员）。
// controller 不可达时状态降级为 "unknown"，不阻断返回。
func (uc *OrderUseCase) ListInstances(ctx context.Context, userID string) ([]UserInstance, error) {
	var orders []*entity.Order
	var err error
	if userID != "" {
		orders, err = uc.repo.ListByUser(ctx, userID)
	} else {
		orders, err = uc.repo.ListAll(ctx)
	}
	if err != nil {
		return nil, err
	}

	out := make([]UserInstance, 0, len(orders))
	for _, o := range orders {
		if o.InstanceID == "" {
			continue
		}
		ui := UserInstance{OrderID: o.ID, InstanceID: o.InstanceID, GameID: o.GameID}
		if inst, err := uc.controller.GetGameInstance(ctx, o.InstanceID); err == nil && inst != nil {
			ui.Status = inst.Status
			if inst.NodeAgentID != nil {
				ui.NodeAgent = *inst.NodeAgentID
			}
		} else {
			ui.Status = "unknown"
		}
		out = append(out, ui)
	}
	return out, nil
}
