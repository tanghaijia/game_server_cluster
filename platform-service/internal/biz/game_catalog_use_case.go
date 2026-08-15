package biz

import (
	"context"
	"errors"
	"time"

	"platform-service/internal/client/controller"
	"platform-service/internal/entity"
	"platform-service/internal/repository"

	"gorm.io/gorm"
)

// GameView 游戏聚合视图：controller 的 Game + platform 的 GameProfile
type GameView struct {
	ID                string               `json:"ID"`
	Name              string               `json:"Name"`
	AppId             string               `json:"AppId"`
	ContainerConfigID string               `json:"ContainerConfigID"`
	Profile           *entity.GameProfile  `json:"profile,omitempty"`
}

// GameCatalogUseCase 游戏目录：聚合 controller games 与 platform game_profiles
type GameCatalogUseCase struct {
	profileRepo repository.GameProfileRepository
	orderRepo   repository.OrderRepository
	controller  *controller.Client
}

func NewGameCatalogUseCase(profileRepo repository.GameProfileRepository, orderRepo repository.OrderRepository, controllerClient *controller.Client) *GameCatalogUseCase {
	return &GameCatalogUseCase{profileRepo: profileRepo, orderRepo: orderRepo, controller: controllerClient}
}

// ListGames 聚合游戏列表。includeDisabled=true（管理员）返回全部 game（含无 profile 的）；
// 否则仅返回 enabled profile 的游戏（用户视角）。
func (uc *GameCatalogUseCase) ListGames(ctx context.Context, includeDisabled bool) ([]GameView, error) {
	games, err := uc.controller.ListGames(ctx)
	if err != nil {
		return nil, err
	}

	var profiles []*entity.GameProfile
	if includeDisabled {
		profiles, err = uc.profileRepo.ListAll(ctx)
	} else {
		profiles, err = uc.profileRepo.ListEnabled(ctx)
	}
	if err != nil {
		return nil, err
	}
	profileByGame := make(map[string]*entity.GameProfile, len(profiles))
	for _, prof := range profiles {
		profileByGame[prof.GameID] = prof
	}

	out := make([]GameView, 0, len(games))
	for _, g := range games {
		if g == nil {
			continue
		}
		prof := profileByGame[g.ID]
		if !includeDisabled && prof == nil {
			continue // 用户视角：未配置/未启用的游戏不可见
		}
		out = append(out, GameView{
			ID:                g.ID,
			Name:              g.Name,
			AppId:             g.AppId,
			ContainerConfigID: g.ContainerConfigID,
			Profile:           prof,
		})
	}
	return out, nil
}

// GetGame 单个游戏详情（用户视角需 profile enabled）
func (uc *GameCatalogUseCase) GetGame(ctx context.Context, gameID string, includeDisabled bool) (*GameView, error) {
	game, err := uc.controller.GetGame(ctx, gameID)
	if err != nil {
		return nil, err
	}
	prof, err := uc.profileRepo.GetByID(ctx, gameID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !includeDisabled && (prof == nil || !prof.Enabled) {
		return nil, controller.ErrNotFound
	}
	return &GameView{
		ID:                game.ID,
		Name:              game.Name,
		AppId:             game.AppId,
		ContainerConfigID: game.ContainerConfigID,
		Profile:           prof,
	}, nil
}

// CreateGame 创建游戏（admin）：调 controller 建 game，并落 profile（可选）
func (uc *GameCatalogUseCase) CreateGame(ctx context.Context, name, appID string, profile *entity.GameProfile) (*GameView, error) {
	game, err := uc.controller.CreateGame(ctx, name, appID)
	if err != nil {
		return nil, err
	}
	if profile != nil && profile.DisplayName != "" {
		profile.GameID = game.ID
		profile.UpdateTime = time.Now()
		if err := uc.profileRepo.Save(ctx, profile); err != nil {
			return nil, err
		}
	}
	return &GameView{
		ID:                game.ID,
		Name:              game.Name,
		AppId:             game.AppId,
		ContainerConfigID: game.ContainerConfigID,
		Profile:           profile,
	}, nil
}

// UpdateProfile 更新游戏资料（admin）；不存在则创建
func (uc *GameCatalogUseCase) UpdateProfile(ctx context.Context, gameID string, updates *entity.GameProfile) (*entity.GameProfile, error) {
	if gameID == "" {
		return nil, errors.New("game_id is required")
	}
	prof, err := uc.profileRepo.GetByID(ctx, gameID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		prof = &entity.GameProfile{GameID: gameID}
	}
	if updates.DisplayName != "" {
		prof.DisplayName = updates.DisplayName
	}
	if updates.IconURL != "" {
		prof.IconURL = updates.IconURL
	}
	if updates.AccentColor != "" {
		prof.AccentColor = updates.AccentColor
	}
	if updates.Description != "" {
		prof.Description = updates.Description
	}
	prof.Enabled = updates.Enabled
	prof.SortOrder = updates.SortOrder
	prof.UpdateTime = time.Now()
	if err := uc.profileRepo.Save(ctx, prof); err != nil {
		return nil, err
	}
	return prof, nil
}

// DeleteGame 删除游戏（admin）：先调 controller 级联删除（存在运行中实例会被拒绝），
// 成功后删除平台侧 game_profiles 并把该游戏未终结订单标记为"已下架"。
func (uc *GameCatalogUseCase) DeleteGame(ctx context.Context, gameID string) error {
	// 1) controller 删除（含实例检查/级联；失败则整体中止）
	if err := uc.controller.DeleteGame(ctx, gameID); err != nil {
		return err
	}
	// 2) 删游戏资料（下架）
	if err := uc.profileRepo.Delete(ctx, gameID); err != nil {
		return err
	}
	// 3) 关联订单标记"已下架"（保留审计）
	if err := uc.orderRepo.MarkGameRemoved(ctx, gameID); err != nil {
		return err
	}
	return nil
}
