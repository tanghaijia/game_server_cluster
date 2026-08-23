package nodeagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	nodeagentv1 "controller-go/internal/third/nodeagent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// NodeAgentFaceClient 封装 protobuf 生成的 NodeAgentServiceClient，
// 为业务层提供稳定的调用入口，隔离底层 gRPC 实现的变更。
type NodeAgentFaceClient struct {
	client nodeagentv1.NodeAgentServiceClient
	conn   *grpc.ClientConn
}

// NewNodeAgentFaceClient 创建封装客户端。
// cc 由调用方管理生命周期，例如从 *grpc.ClientConn 传入。
func NewNodeAgentFaceClient(cc grpc.ClientConnInterface) *NodeAgentFaceClient {
	return &NodeAgentFaceClient{
		client: nodeagentv1.NewNodeAgentServiceClient(cc),
	}
}

// ClientRegistry 管理到多个 NodeAgent 的连接，按需懒加载。
type ClientRegistry struct {
	mu      sync.Mutex
	clients map[string]*NodeAgentFaceClient
}

// NewClientRegistry 创建注册表。
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		clients: make(map[string]*NodeAgentFaceClient),
	}
}

// Get 获取指定 nodeId 的客户端。如果尚未连接，则自动建立连接。
// addr 格式为 "host:port"，例如 "192.168.1.10:9090"。
//
// 注意：连接是异步建立的（不阻塞）。若 node_agent 不可达，后续 RPC 会立即返回
// Unavailable 错误并走调用方的失败/重试流程，而不是让 Dial 永久阻塞——
// 否则实例会卡死在中间态（如 preparing_build），且调度 worker 是单 goroutine，
// 一次阻塞会冻结整个调度队列。
func (r *ClientRegistry) Get(ctx context.Context, nodeId, addr string) (*NodeAgentFaceClient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.clients[nodeId]; ok {
		return c, nil
	}

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// B-12：keepalive 主动探测半开连接，配合调用方 per-RPC 超时，
		// 避免"TCP 能连但应用层不回包"的失联节点让 RPC 永久挂起。
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to node %s at %s: %w", nodeId, addr, err)
	}

	c := &NodeAgentFaceClient{
		client: nodeagentv1.NewNodeAgentServiceClient(conn),
		conn:   conn,
	}
	r.clients[nodeId] = c
	return c, nil
}

// Close 关闭到指定 node 的连接。
func (r *ClientRegistry) Close(nodeId string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.clients[nodeId]; ok {
		delete(r.clients, nodeId)
		return c.conn.Close()
	}
	return nil
}

// CloseAll 关闭所有连接。
func (r *ClientRegistry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, c := range r.clients {
		c.conn.Close()
		delete(r.clients, id)
	}
}

// ---------------------------------------------------------------------------
// 实例生命周期
// ---------------------------------------------------------------------------

func (c *NodeAgentFaceClient) StartInstance(ctx context.Context, in *nodeagentv1.StartInstanceRequest, opts ...grpc.CallOption) (*nodeagentv1.StartInstanceResponse, error) {
	out, err := c.client.StartInstance(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("start instance: %w", err)
	}
	return out, nil
}

func (c *NodeAgentFaceClient) StopInstance(ctx context.Context, in *nodeagentv1.StopInstanceRequest, opts ...grpc.CallOption) (*nodeagentv1.StopInstanceResponse, error) {
	out, err := c.client.StopInstance(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("stop instance: %w", err)
	}
	return out, nil
}

func (c *NodeAgentFaceClient) InspectInstance(ctx context.Context, in *nodeagentv1.InspectInstanceRequest, opts ...grpc.CallOption) (*nodeagentv1.InspectInstanceResponse, error) {
	out, err := c.client.InspectInstance(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("inspect instance: %w", err)
	}
	return out, nil
}

func (c *NodeAgentFaceClient) InspectInstanceStream(ctx context.Context, in *nodeagentv1.InspectInstanceRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[nodeagentv1.InspectInstanceResponse], error) {
	stream, err := c.client.InspectInstanceStream(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("inspect instance stream: %w", err)
	}
	return stream, nil
}

func (c *NodeAgentFaceClient) CleanInstance(ctx context.Context, in *nodeagentv1.CleanInstanceRequest, opts ...grpc.CallOption) (*nodeagentv1.CleanInstanceResponse, error) {
	out, err := c.client.CleanInstance(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("clean instance: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 构建
// ---------------------------------------------------------------------------

func (c *NodeAgentFaceClient) PrepareGameBuild(ctx context.Context, in *nodeagentv1.PrepareGameBuildRequest, opts ...grpc.CallOption) (*nodeagentv1.PrepareGameBuildResponse, error) {
	out, err := c.client.PrepareGameBuild(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("prepare game build: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 快照
// ---------------------------------------------------------------------------

func (c *NodeAgentFaceClient) CreateSnapshot(ctx context.Context, in *nodeagentv1.CreateSnapshotRequest, opts ...grpc.CallOption) (*nodeagentv1.CreateSnapshotResponse, error) {
	out, err := c.client.CreateSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("create snapshot: %w", err)
	}
	return out, nil
}

func (c *NodeAgentFaceClient) RestoreSnapshot(ctx context.Context, in *nodeagentv1.RestoreSnapshotRequest, opts ...grpc.CallOption) (*nodeagentv1.RestoreSnapshotResponse, error) {
	out, err := c.client.RestoreSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("restore snapshot: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

func (c *NodeAgentFaceClient) GetOperation(ctx context.Context, in *nodeagentv1.GetOperationRequest, opts ...grpc.CallOption) (*nodeagentv1.GetOperationResponse, error) {
	out, err := c.client.GetOperation(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get operation: %w", err)
	}
	return out, nil
}

func (c *NodeAgentFaceClient) GetHeartbeat(ctx context.Context, in *nodeagentv1.GetHeartbeatRequest, opts ...grpc.CallOption) (*nodeagentv1.GetHeartbeatResponse, error) {
	out, err := c.client.GetHeartbeat(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get heartbeat: %w", err)
	}
	return out, nil
}

func (c *NodeAgentFaceClient) GetInstances(ctx context.Context, in *nodeagentv1.GetInstancesRequest, opts ...grpc.CallOption) (*nodeagentv1.GetInstancesResponse, error) {
	out, err := c.client.GetInstances(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get instances: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 游戏缓存
// ---------------------------------------------------------------------------

func (c *NodeAgentFaceClient) CacheGame(ctx context.Context, in *nodeagentv1.CacheGameRequest, opts ...grpc.CallOption) (*nodeagentv1.CacheGameResponse, error) {
	out, err := c.client.CacheGame(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("cache game: %w", err)
	}
	return out, nil
}

func (c *NodeAgentFaceClient) GetCacheGame(ctx context.Context, in *nodeagentv1.GetCacheGameRequest, opts ...grpc.CallOption) (*nodeagentv1.CacheGameResponse, error) {
	out, err := c.client.GetCacheGame(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get cache game: %w", err)
	}
	return out, nil
}
