package biz

import (
	"context"
	"errors"
	"time"

	"controller-go/internal/client/assetservice"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	assetservicev1 "controller-go/internal/third/assetservice/v1"
)

// PlatformConfigUseCase 平台运营方配置（control=platform 项，按游戏全局）。
// 创建/启动实例时与 player 配置合并下发（platform 为底、player 覆盖）。
type PlatformConfigUseCase struct {
	repo        repository.GamePlatformConfigRepository
	assetClient *assetservice.AssetServiceFaceClient
}

func NewPlatformConfigUseCase(
	repo repository.GamePlatformConfigRepository,
	assetClient *assetservice.AssetServiceFaceClient,
) *PlatformConfigUseCase {
	return &PlatformConfigUseCase{repo: repo, assetClient: assetClient}
}

// Get 查询某游戏的平台配置（未设置返回空配置，非错误）
func (uc *PlatformConfigUseCase) Get(ctx context.Context, gameID string) (*entity.GamePlatformConfig, error) {
	if gameID == "" {
		return nil, errors.New("game_id is required")
	}
	cfg, err := uc.repo.GetByGame(ctx, gameID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &entity.GamePlatformConfig{GameID: gameID, Config: map[string]string{}}, nil
	}
	return cfg, nil
}

// Update 保存平台配置。校验：只允许 schema 中 control=platform 的 key；
// player/locked 项拒绝（玩家项由用户下单时配置，locked 项平台固定）。
// 无 schema（游戏未注册配置能力）时仅允许空配置。
func (uc *PlatformConfigUseCase) Update(
	ctx context.Context, gameID string, config map[string]string, operator string,
) (*entity.GamePlatformConfig, error) {
	if gameID == "" {
		return nil, errors.New("game_id is required")
	}

	if len(config) > 0 {
		schema, err := LoadGameConfigSchema(ctx, uc.assetClient, gameID)
		if err != nil {
			return nil, err
		}
		// 校验所有 key 都是 platform 项
		platformKeys := make(map[string]bool, len(schema.Settings))
		for _, s := range schema.Settings {
			if s.Control == "platform" {
				platformKeys[s.Key] = true
			}
		}
		for key := range config {
			if !platformKeys[key] {
				return nil, errors.New("配置项 " + key + " 不是平台可配置项（仅 control=platform 的项可在此设置）")
			}
		}
	}

	now := time.Now()
	existing, err := uc.repo.GetByGame(ctx, gameID)
	if err != nil {
		return nil, err
	}
	cfg := &entity.GamePlatformConfig{GameID: gameID, Config: config, UpdateTime: now, UpdatedBy: operator}
	if existing != nil {
		cfg.Version = existing.Version + 1
	}
	if err := uc.repo.Save(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadGameConfigSchema 获取游戏最新构建的配置 schema（供校验/表单生成）。
// 游戏未注册 schema 时返回 error（调用方按"无配置能力"处理）。
func LoadGameConfigSchema(
	ctx context.Context, assetClient *assetservice.AssetServiceFaceClient, gameID string,
) (*AdapterSchema, error) {
	resp, err := assetClient.ListGameBuilds(ctx, &assetservicev1.ListGameBuildsRequest{GameId: gameID})
	if err != nil {
		return nil, err
	}
	for _, b := range resp.Builds {
		if b.GetSchemaJson() != "" {
			return ParseAdapterSchema(b.GetSchemaJson())
		}
	}
	return nil, errors.New("game " + gameID + " has no build with config schema")
}
