package biz

import (
	"context"
	"errors"
	"time"

	"platform-service/internal/entity"
	"platform-service/internal/repository"
)

// OrderUseCase 订单业务逻辑
type OrderUseCase struct {
	repo repository.OrderRepository
}

func NewOrderUseCase(repo repository.OrderRepository) *OrderUseCase {
	return &OrderUseCase{repo: repo}
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

// ListOrders 列出订单；userID 非空时只列该用户的订单
func (uc *OrderUseCase) ListOrders(ctx context.Context, userID string) ([]*entity.Order, error) {
	if userID != "" {
		return uc.repo.ListByUser(ctx, userID)
	}
	return uc.repo.ListAll(ctx)
}
