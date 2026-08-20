package biz

import (
	"context"
	"fmt"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// GameContainerConfigUseCase 游戏容器配置管理（端口/资源默认值/注入模式）：
// 供管理员图形化配置（GET/PUT /api/games/:id/container-config）。
type GameContainerConfigUseCase struct {
	gameRepo   repository.GameRepository
	configRepo repository.GameContainerConfigRepository
}

func NewGameContainerConfigUseCase(
	gameRepo repository.GameRepository,
	configRepo repository.GameContainerConfigRepository,
) *GameContainerConfigUseCase {
	return &GameContainerConfigUseCase{gameRepo: gameRepo, configRepo: configRepo}
}

// GetConfig 返回游戏的容器配置（含端口片段）
func (uc *GameContainerConfigUseCase) GetConfig(ctx context.Context, gameID string) (*entity.GameContainerConfig, error) {
	configID, err := uc.configIDByGame(ctx, gameID)
	if err != nil {
		return nil, err
	}
	return uc.configRepo.GetByID(ctx, configID)
}

// PortExcerptInput 端口片段输入（snake_case）
type PortExcerptInput struct {
	Protocol      entity.ProtocolType `json:"protocol"` // 0=tcp 1=udp
	BeginPort     uint16              `json:"begin_port"`
	ExcerptLength uint16              `json:"excerpt_length"`
	IsGamePort    bool                `json:"is_game_port"`
}

// ContainerConfigUpdate 容器配置更新（指针字段 = 非 nil 才更新；PortExcerpts 非空才整体替换）
type ContainerConfigUpdate struct {
	ContainerServerPath *string                 `json:"container_server_path"`
	PortMode            *entity.GameContainerPortMode `json:"port_mode"` // 0=NAT 1=HOST
	InjectGamePort      *bool                   `json:"inject_game_port"`
	CPURequestMilli     *int64                  `json:"cpu_request_milli"`
	MemoryRequestBytes  *int64                  `json:"memory_request_bytes"`
	DiskRequestBytes    *int64                  `json:"disk_request_bytes"`
	BandwidthRxMbps     *int64                  `json:"bandwidth_rx_mbps"`
	BandwidthTxMbps     *int64                  `json:"bandwidth_tx_mbps"`
	SingleThreaded      *bool                   `json:"single_threaded"`
	PortExcerpts        []PortExcerptInput      `json:"port_excerpts"`
}

// UpdateConfig 更新容器配置并整体替换端口片段
func (uc *GameContainerConfigUseCase) UpdateConfig(ctx context.Context, gameID string, u ContainerConfigUpdate) (*entity.GameContainerConfig, error) {
	configID, err := uc.configIDByGame(ctx, gameID)
	if err != nil {
		return nil, err
	}
	config, err := uc.configRepo.GetByID(ctx, configID)
	if err != nil {
		return nil, err
	}
	if u.ContainerServerPath != nil {
		config.ContainerServerPath = *u.ContainerServerPath
	}
	if u.PortMode != nil {
		config.PortMode = *u.PortMode
	}
	if u.InjectGamePort != nil {
		config.InjectGamePort = *u.InjectGamePort
	}
	if u.CPURequestMilli != nil {
		config.CPURequestMilli = *u.CPURequestMilli
	}
	if u.MemoryRequestBytes != nil {
		config.MemoryRequestBytes = *u.MemoryRequestBytes
	}
	if u.DiskRequestBytes != nil {
		config.DiskRequestBytes = *u.DiskRequestBytes
	}
	if u.BandwidthRxMbps != nil {
		config.BandwidthRxMbps = *u.BandwidthRxMbps
	}
	if u.BandwidthTxMbps != nil {
		config.BandwidthTxMbps = *u.BandwidthTxMbps
	}
	if u.SingleThreaded != nil {
		config.SingleThreaded = *u.SingleThreaded
	}
	if err := uc.configRepo.Save(ctx, config); err != nil {
		return nil, err
	}
	if u.PortExcerpts != nil {
		excerpts := make([]entity.GameContainerPortExcerpt, 0, len(u.PortExcerpts))
		for _, e := range u.PortExcerpts {
			excerpts = append(excerpts, entity.GameContainerPortExcerpt{
				Protocol:      e.Protocol,
				BeginPort:     e.BeginPort,
				ExcerptLength: e.ExcerptLength,
				IsGamePort:    e.IsGamePort,
			})
		}
		if err := uc.configRepo.ReplacePortExcerpts(ctx, config.ID, excerpts); err != nil {
			return nil, err
		}
	}
	return uc.configRepo.GetByID(ctx, config.ID)
}

func (uc *GameContainerConfigUseCase) configIDByGame(ctx context.Context, gameID string) (string, error) {
	game, err := uc.gameRepo.GetByID(ctx, gameID)
	if err != nil {
		return "", err
	}
	if game.ContainerConfigID == "" {
		return "", fmt.Errorf("game %s 未配置容器配置（container_config_id 为空）", gameID)
	}
	return game.ContainerConfigID, nil
}
