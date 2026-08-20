package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
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
 * 分配的宿主机端口介于TCP_PORT_BEGIN-TCP_PORT_END和UDP_PORT_BEGIN-UDP_PORT_END之间。
 *
 * 关键规则：同一容器端口同时声明了 TCP 与 UDP（如 7DTD 的 26900 游戏端口，
 * LiteNetLib 走 UDP、rules 查询走 TCP）时，TCP/UDP 必须映射到【同一个宿主端口】，
 * 否则客户端连接宿主端口时一种协议打不到服务器。因此先按容器端口归并协议需求，
 * 再为每个容器端口分配宿主端口。
 *
 * MapPort = 幂等释放 + PlanPorts（计算）+ 写库；
 * PlanPorts 只计算不写库（H4 预检 / 预留事务内写入用，§6.1/§7.1）。
 */
func (g *GameContainerPortMapper) MapPort(
	ctx context.Context, nodeAgent *entity.NodeAgent, game *entity.Game,
	gameInstance *entity.GameInstance) ([]entity.ContainerPortMapping, error) {

	// 幂等：若该实例已分配过端口（例如失败重试、程序重启恢复），先释放再重新分配
	if err := g.releaseIfExists(ctx, gameInstance.ID); err != nil {
		return nil, err
	}
	mappings, err := g.PlanPorts(ctx, nodeAgent, game, gameInstance)
	if err != nil {
		return nil, err
	}
	for i := range mappings {
		if err := g.portMappingRepo.Save(ctx, &mappings[i]); err != nil {
			return nil, fmt.Errorf("save port mapping: %w", err)
		}
	}
	return mappings, nil
}

// PlanPorts 计算端口映射方案（不写库、不释放既有映射）。
// 供硬约束 H4 预检（§6.1）与预留事务内写入（§7.1）使用。
func (g *GameContainerPortMapper) PlanPorts(
	ctx context.Context, nodeAgent *entity.NodeAgent, game *entity.Game,
	gameInstance *entity.GameInstance) ([]entity.ContainerPortMapping, error) {

	config, err := g.gameContainerConfigRepo.GetByID(ctx, game.ContainerConfigID)
	if err != nil {
		return nil, fmt.Errorf("load container config %s: %w", game.ContainerConfigID, err)
	}

	// 端口注入模式：游戏端口 = 分配的宿主端口（identity 映射），
	// 通过 env 注入 adapter（如 SDTD_SERVER_PORT=H），由 start.sh 改写游戏配置。
	// 这样游戏通告的端口 == 宿主映射端口，EOS/Steam 发现与直连一致。
	if config.InjectGamePort && config.PortMode == entity.PORT_MAPPING_MOD_NAT {
		return g.planPortsInjected(ctx, nodeAgent, gameInstance, config)
	}
	return g.planPortsNAT(ctx, nodeAgent, gameInstance, config)
}

// planPortsNAT 普通 NAT/HOST 模式的端口方案计算（原 MapPort 计算主体，不写库）
func (g *GameContainerPortMapper) planPortsNAT(
	ctx context.Context, nodeAgent *entity.NodeAgent, gameInstance *entity.GameInstance,
	config *entity.GameContainerConfig) ([]entity.ContainerPortMapping, error) {

	// 该 node_agent 上已占用的宿主端口（按协议区分，TCP/UDP 端口空间独立）
	usedByNode, err := g.portMappingRepo.ListByNodeAgentId(ctx, nodeAgent.ID)
	if err != nil {
		return nil, fmt.Errorf("list port mappings of node agent %s: %w", nodeAgent.ID, err)
	}
	used := usedHostPorts(usedByNode)

	// 归并：容器端口 -> 需要的协议集合（同一端口 TCP+UDP 都要时合并）
	type portRequirement struct {
		tcp bool
		udp bool
	}
	requirements := make(map[uint16]*portRequirement)
	var containerPorts []uint16
	for _, excerpt := range config.PortExcerpt {
		for i := uint16(0); i < excerpt.ExcerptLength; i++ {
			cp := excerpt.BeginPort + i
			req := requirements[cp]
			if req == nil {
				req = &portRequirement{}
				requirements[cp] = req
				containerPorts = append(containerPorts, cp)
			}
			switch excerpt.Protocol {
			case entity.TCP:
				req.tcp = true
			case entity.UDP:
				req.udp = true
			}
		}
	}
	// 稳定排序，保证分配顺序确定（HOST 模式按端口号从低到高检测占用）
	sort.Slice(containerPorts, func(i, j int) bool { return containerPorts[i] < containerPorts[j] })

	var mappings []entity.ContainerPortMapping
	for _, containerPort := range containerPorts {
		req := requirements[containerPort]
		if req == nil {
			continue
		}

		var hostPort uint16
		if config.PortMode == entity.PORT_MAPPING_MOD_HOST {
			// HOST 模式：容器直接占用宿主端口，host 端口即容器端口，所有需要的协议都必须未被占用
			hostPort = containerPort
			if (req.tcp && portInUse(used, entity.TCP, hostPort)) ||
				(req.udp && portInUse(used, entity.UDP, hostPort)) {
				return nil, fmt.Errorf(
					"host port %d already in use on node agent %s",
					hostPort, nodeAgent.ID)
			}
		} else {
			// NAT 模式：从空闲端口段中挑选一个未被占用的宿主端口。
			// TCP+UDP 同端口时要求该端口在两种协议空间都空闲（共用同一端口号）。
			hostPort, err = g.allocateHostPort(req.tcp, req.udp, used)
			if err != nil {
				return nil, fmt.Errorf("allocate host port for container port %d: %w", containerPort, err)
			}
		}

		if req.tcp {
			markUsed(used, entity.TCP, hostPort)
			mappings = append(mappings, entity.ContainerPortMapping{
				ID:            newPortMappingID(),
				InstanceId:    gameInstance.ID,
				NodeAgentId:   nodeAgent.ID,
				HostPort:      hostPort,
				ContainerPort: containerPort,
				Protocol:      entity.TCP,
			})
		}
		if req.udp {
			markUsed(used, entity.UDP, hostPort)
			mappings = append(mappings, entity.ContainerPortMapping{
				ID:            newPortMappingID(),
				InstanceId:    gameInstance.ID,
				NodeAgentId:   nodeAgent.ID,
				HostPort:      hostPort,
				ContainerPort: containerPort,
				Protocol:      entity.UDP,
			})
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

// allocateHostPort 分配宿主端口：
//   - 仅需 TCP 或仅需 UDP：在对应协议端口段内找第一个未被占用的端口；
//   - TCP+UDP 都需：在端口段内找第一个两种协议空间都空闲的端口（共用同一端口号）。
// 调用方负责 markUsed 落占用标记（分配函数本身不改 used，便于调用方按协议分别标记）。
func (g *GameContainerPortMapper) allocateHostPort(
	needTCP, needUDP bool, used map[entity.ProtocolType]map[uint16]struct{}) (uint16, error) {
	begin, end := uint16(TCP_PORT_BEGIN), uint16(TCP_PORT_END)
	if needTCP && !needUDP {
		if used[entity.TCP] == nil {
			used[entity.TCP] = make(map[uint16]struct{})
		}
		for port := begin; port <= end; port++ {
			if _, ok := used[entity.TCP][port]; !ok {
				return port, nil
			}
		}
	} else if needUDP && !needTCP {
		begin, end = uint16(UDP_PORT_BEGIN), uint16(UDP_PORT_END)
		if used[entity.UDP] == nil {
			used[entity.UDP] = make(map[uint16]struct{})
		}
		for port := begin; port <= end; port++ {
			if _, ok := used[entity.UDP][port]; !ok {
				return port, nil
			}
		}
	} else {
		// TCP+UDP 共用：两种协议空间都要空闲
		if used[entity.TCP] == nil {
			used[entity.TCP] = make(map[uint16]struct{})
		}
		if used[entity.UDP] == nil {
			used[entity.UDP] = make(map[uint16]struct{})
		}
		for port := begin; port <= end; port++ {
			_, tcpTaken := used[entity.TCP][port]
			_, udpTaken := used[entity.UDP][port]
			if !tcpTaken && !udpTaken {
				return port, nil
			}
		}
	}
	return 0, errors.New("no free host port available")
}

// portInUse 判断某协议空间内端口是否已占用（未初始化视为空闲）
func portInUse(used map[entity.ProtocolType]map[uint16]struct{}, protocol entity.ProtocolType, port uint16) bool {
	if used[protocol] == nil {
		return false
	}
	_, ok := used[protocol][port]
	return ok
}

// markUsed 将端口标记为已占用（初始化协议空间）
func markUsed(used map[entity.ProtocolType]map[uint16]struct{}, protocol entity.ProtocolType, port uint16) {
	if used[protocol] == nil {
		used[protocol] = make(map[uint16]struct{})
	}
	used[protocol][port] = struct{}{}
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

// planPortsInjected 端口注入模式（方案A，只计算不写库）：
//   - 游戏端口块（is_game_port 起、覆盖到所有 >= 基准端口的 excerpt）分配一段连续宿主端口，
//     并做 identity 映射（container_port == host_port），因为游戏会被注入 SDTD_SERVER_PORT=H，
//     容器内监听端口就是 H，不再是模板里的固定端口。
//   - 块内每个端口按需生成 TCP/UDP 两条映射（同宿主端口）。
//   - 基准端口以下的 excerpt（如 telnet 8081）保持普通 NAT 映射。
// 返回的映射中，游戏端口块基准对应的 TCP 映射标记 IsGamePort=true（供 connect/env 定位宿主端口）。
func (g *GameContainerPortMapper) planPortsInjected(
	ctx context.Context, nodeAgent *entity.NodeAgent, gameInstance *entity.GameInstance,
	config *entity.GameContainerConfig,
) ([]entity.ContainerPortMapping, error) {

	// 1. 找游戏端口基准（is_game_port 标记的 excerpt）
	gameBase := uint16(0)
	for _, e := range config.PortExcerpt {
		if e.IsGamePort {
			gameBase = e.BeginPort
			break
		}
	}
	if gameBase == 0 {
		return nil, errors.New("inject_game_port config has no is_game_port excerpt")
	}

	// 2. 计算游戏端口块长度：覆盖所有 begin_port >= gameBase 的 excerpt 的最大范围
	var blockLen uint16 = 1
	for _, e := range config.PortExcerpt {
		if e.BeginPort >= gameBase {
			end := e.BeginPort + e.ExcerptLength
			if end > gameBase+blockLen {
				blockLen = end - gameBase
			}
		}
	}

	// 3. 该 node_agent 上已占用的宿主端口
	usedByNode, err := g.portMappingRepo.ListByNodeAgentId(ctx, nodeAgent.ID)
	if err != nil {
		return nil, fmt.Errorf("list port mappings of node agent %s: %w", nodeAgent.ID, err)
	}
	used := usedHostPorts(usedByNode)

	// 4. 分配连续宿主端口段（块内所有端口在 TCP/UDP 空间都须空闲，因为 identity 且同号共用）
	hostBase, err := g.allocateContiguousHostPorts(blockLen, used)
	if err != nil {
		return nil, fmt.Errorf("allocate contiguous host ports for game port block: %w", err)
	}
	for i := uint16(0); i < blockLen; i++ {
		markUsed(used, entity.TCP, hostBase+i)
		markUsed(used, entity.UDP, hostBase+i)
	}

	// 5. 生成块内 identity 映射（按端口协议需求）
	var mappings []entity.ContainerPortMapping
	for i := uint16(0); i < blockLen; i++ {
		containerPort := gameBase + i
		hostPort := hostBase + i
		needTCP, needUDP := false, false
		for _, e := range config.PortExcerpt {
			if e.BeginPort <= containerPort && containerPort < e.BeginPort+e.ExcerptLength {
				switch e.Protocol {
				case entity.TCP:
					needTCP = true
				case entity.UDP:
					needUDP = true
				}
			}
		}
		if needTCP {
			mappings = append(mappings, entity.ContainerPortMapping{
				ID:            newPortMappingID(),
				InstanceId:    gameInstance.ID,
				NodeAgentId:   nodeAgent.ID,
				HostPort:      hostPort,
				ContainerPort: hostPort, // identity
				Protocol:      entity.TCP,
				IsGamePort:    containerPort == gameBase,
			})
		}
		if needUDP {
			mappings = append(mappings, entity.ContainerPortMapping{
				ID:            newPortMappingID(),
				InstanceId:    gameInstance.ID,
				NodeAgentId:   nodeAgent.ID,
				HostPort:      hostPort,
				ContainerPort: hostPort, // identity
				Protocol:      entity.UDP,
			})
		}
	}

	// 6. 基准端口以下的 excerpt（如 telnet）走普通 NAT
	extra, err := g.mapPortExtraNAT(ctx, nodeAgent, gameInstance, config, gameBase, used)
	if err != nil {
		return nil, err
	}
	mappings = append(mappings, extra...)

	return mappings, nil
}

// mapPortExtraNAT 为游戏端口块以外的 excerpt（begin_port < gameBase）做普通 NAT 映射
func (g *GameContainerPortMapper) mapPortExtraNAT(
	ctx context.Context, nodeAgent *entity.NodeAgent, gameInstance *entity.GameInstance,
	config *entity.GameContainerConfig, gameBase uint16, used map[entity.ProtocolType]map[uint16]struct{},
) ([]entity.ContainerPortMapping, error) {
	var mappings []entity.ContainerPortMapping
	for _, excerpt := range config.PortExcerpt {
		if excerpt.BeginPort >= gameBase {
			continue // 已在注入块内
		}
		for i := uint16(0); i < excerpt.ExcerptLength; i++ {
			containerPort := excerpt.BeginPort + i
			hostPort, err := g.allocateHostPort(excerpt.Protocol == entity.TCP, excerpt.Protocol == entity.UDP, used)
			if err != nil {
				return nil, fmt.Errorf("allocate host port for container port %d: %w", containerPort, err)
			}
			if excerpt.Protocol == entity.TCP {
				markUsed(used, entity.TCP, hostPort)
			} else {
				markUsed(used, entity.UDP, hostPort)
			}
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
	return mappings, nil
}

// allocateContiguousHostPorts 分配一段连续的宿主端口（TCP/UDP 空间都空闲，供 identity 映射）
func (g *GameContainerPortMapper) allocateContiguousHostPorts(
	count uint16, used map[entity.ProtocolType]map[uint16]struct{}) (uint16, error) {
	if used[entity.TCP] == nil {
		used[entity.TCP] = make(map[uint16]struct{})
	}
	if used[entity.UDP] == nil {
		used[entity.UDP] = make(map[uint16]struct{})
	}
	begin, end := uint16(TCP_PORT_BEGIN), uint16(TCP_PORT_END)
	if end-begin+1 < count {
		return 0, errors.New("port range too small for contiguous block")
	}
	for start := begin; start+count-1 <= end; start++ {
		free := true
		for i := uint16(0); i < count; i++ {
			p := start + i
			if _, ok := used[entity.TCP][p]; ok {
				free = false
				break
			}
			if _, ok := used[entity.UDP][p]; ok {
				free = false
				break
			}
		}
		if free {
			return start, nil
		}
	}
	return 0, errors.New("no free contiguous host ports available")
}
