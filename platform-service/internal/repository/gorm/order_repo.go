package gorm

import (
	"context"

	"platform-service/internal/entity"

	"gorm.io/gorm"
)

type OrderRepo struct {
	db *gorm.DB
}

func NewOrderRepo(db *gorm.DB) *OrderRepo {
	return &OrderRepo{db: db}
}

func (r *OrderRepo) Save(ctx context.Context, order *entity.Order) error {
	return r.db.WithContext(ctx).Save(order).Error
}

func (r *OrderRepo) GetByID(ctx context.Context, id string) (*entity.Order, error) {
	var order entity.Order
	err := r.db.WithContext(ctx).First(&order, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (r *OrderRepo) ListByUser(ctx context.Context, userID string) ([]*entity.Order, error) {
	var orders []*entity.Order
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("create_time").Find(&orders).Error
	if err != nil {
		return nil, err
	}
	return orders, nil
}

func (r *OrderRepo) ListByGame(ctx context.Context, gameID string) ([]*entity.Order, error) {
	var orders []*entity.Order
	err := r.db.WithContext(ctx).Where("game_id = ?", gameID).Order("create_time").Find(&orders).Error
	if err != nil {
		return nil, err
	}
	return orders, nil
}

func (r *OrderRepo) ListByUserAndGame(ctx context.Context, userID, gameID string) ([]*entity.Order, error) {
	var orders []*entity.Order
	err := r.db.WithContext(ctx).Where("user_id = ? AND game_id = ?", userID, gameID).Order("create_time").Find(&orders).Error
	if err != nil {
		return nil, err
	}
	return orders, nil
}

func (r *OrderRepo) MarkGameRemoved(ctx context.Context, gameID string) error {
	// 标记未终结订单（created/paid/provisioned）；已取消/已退款保留历史
	return r.db.WithContext(ctx).Model(&entity.Order{}).
		Where("game_id = ? AND status IN ?", gameID, []entity.OrderStatus{
			entity.OrderStatusCreated, entity.OrderStatusPaid, entity.OrderStatusProvisioned,
		}).
		Update("status", entity.OrderStatusGameRemoved).Error
}

func (r *OrderRepo) ListAll(ctx context.Context) ([]*entity.Order, error) {
	var orders []*entity.Order
	err := r.db.WithContext(ctx).Order("create_time").Find(&orders).Error
	if err != nil {
		return nil, err
	}
	return orders, nil
}
