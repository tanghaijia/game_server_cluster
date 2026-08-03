package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

type GameContainerPortMapper struct {
	portMappingRepo         repository.ContainerPortMappingRepository
	gameContainerConfigRepo repository.GameContainerConfigRepository
}

func NewGameContainerPortMapper(
	portMappingRepo repository.ContainerPortMappingRepository,
	gameContainerConfigRepo repository.GameContainerConfigRepository,
) *GameContainerPortMapper {
	return &GameContainerPortMapper{
		portMappingRepo:         portMappingRepo,
		gameContainerConfigRepo: gameContainerConfigRepo,
	}
}

/**
 * 根据game.ContainerConfig的需求做动态的端口映射，
 * 把容器内的端口找到一个没被占用的宿主机端口,
 * 分配的宿主机端口介于TCP_PORT_BEGIN-TCP_PORT_END和UDP_PORT_BEGIN-UDP_PORT_END之间
 */
func (g *GameContainerPortMapper) MapPort(
	ctx context.Context, nodeAgent *entity.NodeAgent, game *entity.Game,
	gameInstance *entity.GameInstance) ([]entity.ContainerPortMapping, error) {

	// 幂等：若该实例已分配过端口（例如失败重试、程序重启恢复），先释放再重新分配
	if err := g.releaseIfExists(ctx, gameInstance.ID); err != nil {
		return nil, err
	}

	config, err := g.gameContainerConfigRepo.GetByID(ctx, game.ContainerConfigID)
	if err != nil {
		return nil, fmt.Errorf("load container config %s: %w", game.ContainerConfigID, err)
	}

	// 该 node_agent 上已占用的宿主端口（按协议区分，TCP/UDP 端口空间独立）
	usedByNode, err := g.portMappingRepo.ListByNodeAgentId(ctx, nodeAgent.ID)
	if err != nil {
		return nil, fmt.Errorf("list port mappings of node agent %s: %w", nodeAgent.ID, err)
	}
	used := usedHostPorts(usedByNode)

	var mappings []entity.ContainerPortMapping
	for _, excerpt := range config.PortExcerpt {
		for i := uint16(0); i < excerpt.ExcerptLength; i++ {
			containerPort := excerpt.BeginPort + i

			var hostPort uint16
			if config.PortMode == entity.PORT_MAPPING_MOD_HOST {
				// HOST 模式：容器直接占用宿主端口，host 端口即容器端口，必须未被占用
				hostPort = containerPort
				if _, ok := used[excerpt.Protocol][hostPort]; ok {
					return nil, fmt.Errorf(
						"host port %d (protocol %v) already in use on node agent %s",
						hostPort, excerpt.Protocol, nodeAgent.ID)
				}
			} else {
				// NAT 模式：从空闲端口段中挑选一个未被占用的宿主端口
				hostPort, err = g.allocateHostPort(excerpt.Protocol, used)
				if err != nil {
					return nil, fmt.Errorf("allocate host port for container port %d: %w", containerPort, err)
				}
			}
			if used[excerpt.Protocol] == nil {
				used[excerpt.Protocol] = make(map[uint16]struct{})
			}
			used[excerpt.Protocol][hostPort] = struct{}{}

			mappings = append(mappings, entity.ContainerPortMapping{
				ID:            newPortMappingID(),
				InstanceId:    gameInstance.ID,
				NodeAgentId:   nodeAgent.ID,
				HostPort:      hostPort,
				ContainerPort: containerPort,
				Protocol:      excerpt.Protocol,
			})
		}
	}

	for i := range mappings {
		if err := g.portMappingRepo.Save(ctx, &mappings[i]); err != nil {
			return nil, fmt.Errorf("save port mapping: %w", err)
		}
	}
	return mappings, nil
}

// releaseIfExists 释放实例已占用的端口，保证 MapPort 幂等
func (g *GameContainerPortMapper) releaseIfExists(ctx context.Context, gameInstanceId string) error {
	existing, err := g.portMappingRepo.ListByInstanceId(ctx, gameInstanceId)
	if err != nil {
		return fmt.Errorf("list port mappings of instance %s: %w", gameInstanceId, err)
	}
	if len(existing) == 0 {
		return nil
	}
	return g.portMappingRepo.DeleteByInstanceId(ctx, gameInstanceId)
}

// allocateHostPort 在对应协议端口段内寻找第一个未被占用的端口，并将其标记为已占用
func (g *GameContainerPortMapper) allocateHostPort(
	protocol entity.ProtocolType, used map[entity.ProtocolType]map[uint16]struct{}) (uint16, error) {
	begin, end := uint16(TCP_PORT_BEGIN), uint16(TCP_PORT_END)
	if protocol == entity.UDP {
		begin, end = uint16(UDP_PORT_BEGIN), uint16(UDP_PORT_END)
	}
	if used[protocol] == nil {
		used[protocol] = make(map[uint16]struct{})
	}
	for port := begin; port <= end; port++ {
		if _, ok := used[protocol][port]; !ok {
			used[protocol][port] = struct{}{}
			return port, nil
		}
	}
	return 0, errors.New("no free host port available")
}

// usedHostPorts 汇总已有映射中的宿主端口占用情况（按协议区分）
func usedHostPorts(mappings []*entity.ContainerPortMapping) map[entity.ProtocolType]map[uint16]struct{} {
	used := make(map[entity.ProtocolType]map[uint16]struct{})
	for _, m := range mappings {
		if used[m.Protocol] == nil {
			used[m.Protocol] = make(map[uint16]struct{})
		}
		used[m.Protocol][m.HostPort] = struct{}{}
	}
	return used
}

/*
 * 释放GameInstance占用的端口
 */
func (g *GameContainerPortMapper) ReleaseMapPortByInstanceId(
	ctx context.Context, gameInstanceId string) ([]entity.ContainerPortMapping, error) {
	mappings, err := g.portMappingRepo.ListByInstanceId(ctx, gameInstanceId)
	if err != nil {
		return nil, fmt.Errorf("list port mappings of instance %s: %w", gameInstanceId, err)
	}
	if len(mappings) == 0 {
		return nil, nil
	}
	if err := g.portMappingRepo.DeleteByInstanceId(ctx, gameInstanceId); err != nil {
		return nil, fmt.Errorf("delete port mappings of instance %s: %w", gameInstanceId, err)
	}
	return derefMappings(mappings), nil
}

func (g *GameContainerPortMapper) GetMapPortByNodeAgentId(
	ctx context.Context, nodeAgentId string) ([]entity.ContainerPortMapping, error) {
	mappings, err := g.portMappingRepo.ListByNodeAgentId(ctx, nodeAgentId)
	if err != nil {
		return nil, fmt.Errorf("list port mappings of node agent %s: %w", nodeAgentId, err)
	}
	return derefMappings(mappings), nil
}

func (g *GameContainerPortMapper) GetMapPortByInstanceId(
	ctx context.Context, gameInstanceId string) ([]entity.ContainerPortMapping, error) {
	mappings, err := g.portMappingRepo.ListByInstanceId(ctx, gameInstanceId)
	if err != nil {
		return nil, fmt.Errorf("list port mappings of instance %s: %w", gameInstanceId, err)
	}
	return derefMappings(mappings), nil
}

// derefMappings 将指针切片转为值切片
func derefMappings(mappings []*entity.ContainerPortMapping) []entity.ContainerPortMapping {
	result := make([]entity.ContainerPortMapping, 0, len(mappings))
	for _, m := range mappings {
		result = append(result, *m)
	}
	return result
}

// newPortMappingID 生成唯一端口映射ID
func newPortMappingID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("port-map-%d", time.Now().UnixNano())
	}
	return "port-map-" + hex.EncodeToString(b)
}
