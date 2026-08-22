package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GameInstance controller-go 返回的实例（JSON 字段为 PascalCase，Go 解码对大小写不敏感）
type GameInstance struct {
	ID          string  `json:"ID"`
	GameID      string  `json:"GameID"`
	NodeAgentID *string `json:"NodeAgentID"`
	Status      string  `json:"Status"`
	GameBuildId string  `json:"GameBuildId"`
	FailReason  string  `json:"FailReason"` // 失败原因（调度/阶段失败等，前端展示）
}

// Client controller-go HTTP 客户端（ADR-0001：platform-service 编排 controller）
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8088"
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// ErrNotFound controller 返回 404（资源不存在）
var ErrNotFound = errors.New("controller: not found")

// ContainerConfig 游戏容器配置（controller 返回，PascalCase JSON）
type ContainerConfig struct {
	ID                  string `json:"ID"`
	ContainerServerPath string `json:"ContainerServerPath"`
	PortMode            int    `json:"PortMode"` // 0=NAT 1=HOST
	InjectGamePort      bool   `json:"InjectGamePort"`
	CPURequestMilli     int64  `json:"CPURequestMilli"`
	MemoryRequestBytes  int64  `json:"MemoryRequestBytes"`
	DiskRequestBytes    int64  `json:"DiskRequestBytes"`
	BandwidthRxMbps     int64  `json:"BandwidthRxMbps"`
	BandwidthTxMbps     int64  `json:"BandwidthTxMbps"`
	SingleThreaded      bool   `json:"SingleThreaded"`
	PortExcerpt         []struct {
		Protocol      int  `json:"Protocol"` // 0=tcp 1=udp
		BeginPort     uint `json:"BeginPort"`
		ExcerptLength uint `json:"ExcerptLength"`
		IsGamePort    bool `json:"IsGamePort"`
	} `json:"PortExcerpt"`
}

// ContainerConfigUpdate 容器配置更新（snake_case 输入，与 controller 对齐）
type ContainerConfigUpdate struct {
	ContainerServerPath *string `json:"container_server_path"`
	PortMode            *int    `json:"port_mode"`
	InjectGamePort      *bool   `json:"inject_game_port"`
	CPURequestMilli     *int64  `json:"cpu_request_milli"`
	MemoryRequestBytes  *int64  `json:"memory_request_bytes"`
	DiskRequestBytes    *int64  `json:"disk_request_bytes"`
	BandwidthRxMbps     *int64  `json:"bandwidth_rx_mbps"`
	BandwidthTxMbps     *int64  `json:"bandwidth_tx_mbps"`
	SingleThreaded      *bool   `json:"single_threaded"`
	PortExcerpts        []struct {
		Protocol      int  `json:"protocol"`
		BeginPort     uint `json:"begin_port"`
		ExcerptLength uint `json:"excerpt_length"`
		IsGamePort    bool `json:"is_game_port"`
	} `json:"port_excerpts"`
}

// GetContainerConfig 获取游戏容器配置
func (c *Client) GetContainerConfig(ctx context.Context, gameID string) (*ContainerConfig, error) {
	var cfg ContainerConfig
	if err := c.do(ctx, http.MethodGet, "/api/games/"+gameID+"/container-config", nil, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateContainerConfig 更新游戏容器配置（端口片段整体替换）
func (c *Client) UpdateContainerConfig(ctx context.Context, gameID string, u ContainerConfigUpdate) (*ContainerConfig, error) {
	var cfg ContainerConfig
	if err := c.do(ctx, http.MethodPut, "/api/games/"+gameID+"/container-config", u, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// CreateGameInstance 在 controller 上创建实例（初始 stopped）。
// config 为实例配置（游戏配置 schema 声明的键值，nil 表示不传）。
func (c *Client) CreateGameInstance(ctx context.Context, gameID, buildID string, config map[string]string) (*GameInstance, error) {
	body := map[string]any{"game_id": gameID}
	if buildID != "" {
		body["game_build_id"] = buildID
	}
	if len(config) > 0 {
		body["config"] = config
	}
	var inst GameInstance
	if err := c.do(ctx, http.MethodPost, "/api/game-instances", body, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// ConfigSchema 游戏配置 schema（controller 透传 asset_service，供前端表单生成）
type ConfigSchema struct {
	GameID          string         `json:"game_id"`
	BuildID         string         `json:"build_id"`
	SchemaJSON      string         `json:"schema_json"`
	AdapterMetadata map[string]any `json:"adapter_metadata,omitempty"`
}

// GetConfigSchema 获取游戏配置 schema（controller /api/games/:id/config-schema）
func (c *Client) GetConfigSchema(ctx context.Context, gameID string) (*ConfigSchema, error) {
	var s ConfigSchema
	if err := c.do(ctx, http.MethodGet, "/api/games/"+gameID+"/config-schema", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// PlatformConfig 平台运营方配置（按游戏全局，control=platform 项）
type PlatformConfig struct {
	GameID     string            `json:"GameID"`
	Config     map[string]string `json:"Config"`
	Version    int64             `json:"Version"`
	UpdatedBy  string            `json:"UpdatedBy"`
	UpdateTime string            `json:"UpdateTime"`
}

// GetPlatformConfig 获取平台运营方配置（controller /api/games/:id/platform-config）
func (c *Client) GetPlatformConfig(ctx context.Context, gameID string) (*PlatformConfig, error) {
	var pc PlatformConfig
	if err := c.do(ctx, http.MethodGet, "/api/games/"+gameID+"/platform-config", nil, &pc); err != nil {
		return nil, err
	}
	return &pc, nil
}

// UpdatePlatformConfig 更新平台运营方配置（仅 control=platform 的 key 允许）
func (c *Client) UpdatePlatformConfig(ctx context.Context, gameID string, config map[string]string) (*PlatformConfig, error) {
	var pc PlatformConfig
	if err := c.do(ctx, http.MethodPut, "/api/games/"+gameID+"/platform-config",
		map[string]any{"config": config}, &pc); err != nil {
		return nil, err
	}
	return &pc, nil
}

// UpdateInstanceConfig 更新实例配置（controller 校验 schema 后落库，重启生效）
func (c *Client) UpdateInstanceConfig(ctx context.Context, instanceID string, config map[string]string) (*GameInstance, error) {
	var inst GameInstance
	if err := c.do(ctx, http.MethodPut, "/api/game-instances/"+instanceID+"/config",
		map[string]any{"config": config}, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// StartGameInstance 启动实例（进入调度）
func (c *Client) StartGameInstance(ctx context.Context, instanceID string) error {
	return c.do(ctx, http.MethodPost, "/api/game-instances/"+instanceID+"/start", nil, nil)
}

// GetGameInstance 查询实例状态
func (c *Client) GetGameInstance(ctx context.Context, instanceID string) (*GameInstance, error) {
	var inst GameInstance
	if err := c.do(ctx, http.MethodGet, "/api/game-instances/"+instanceID, nil, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// StopGameInstance 停止实例（进入调度）
func (c *Client) StopGameInstance(ctx context.Context, instanceID string) error {
	return c.do(ctx, http.MethodPost, "/api/game-instances/"+instanceID+"/stop", nil, nil)
}

// InstanceConnect controller 返回的实例连接信息（connect_address = node_ip:game_host_port）
type InstanceConnect struct {
	NodeIP        string `json:"node_ip"`
	GameHostPort  uint16 `json:"game_host_port"`
	GamePort      uint16 `json:"game_port"`
	Protocol      string `json:"protocol"`
}

// GetInstanceConnect 查询实例对外连接信息（node_ip + 游戏端口宿主端口）
func (c *Client) GetInstanceConnect(ctx context.Context, instanceID string) (*InstanceConnect, error) {
	var info InstanceConnect
	if err := c.do(ctx, http.MethodGet, "/api/game-instances/"+instanceID+"/connect", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ---------------------------------------------------------------------------
// 文件会话（M2，见 docs/file-manager-design.md）
// ---------------------------------------------------------------------------

// FileSession controller 签发的文件会话（浏览器直连 node_agent 文件服务所需）
type FileSession struct {
	BaseURL    string `json:"base_url"`
	Token      string `json:"token"`
	InstanceID string `json:"instance_id"`
	DataRoot   string `json:"data_root"`
	ExpiresAt  string `json:"expires_at"`
}

// CreateFileSession 获取实例的文件会话（controller 签发短效 JWT）
func (c *Client) CreateFileSession(ctx context.Context, instanceID string) (*FileSession, error) {
	var s FileSession
	if err := c.do(ctx, http.MethodPost, "/api/game-instances/"+instanceID+"/file-session", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ---------------------------------------------------------------------------
// Node（管理员管理）
// ---------------------------------------------------------------------------

type Node struct {
	Id              int64   `json:"Id"`
	Ip              string  `json:"Ip"`
	CoreNum         int     `json:"CoreNum"`
	CoreFrequency   float64 `json:"CoreFrequency"`
	MemorySize      int64   `json:"MemorySize"`
	StorageSize     int64   `json:"StorageSize"`
	Location        string  `json:"Location"`
	ServiceProvider string  `json:"ServiceProvider"`
	NetRxLimitMbps  int     `json:"NetRxLimitMbps"` // 带宽上限（调度评分）
	NetTxLimitMbps  int     `json:"NetTxLimitMbps"`
}

func (c *Client) CreateNode(ctx context.Context, ip string) (*Node, error) {
	var n Node
	if err := c.do(ctx, http.MethodPost, "/api/nodes", map[string]string{"ip": ip}, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func (c *Client) ListNodes(ctx context.Context) ([]*Node, error) {
	var out struct {
		Nodes []*Node `json:"nodes"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/nodes", nil, &out); err != nil {
		return nil, err
	}
	return out.Nodes, nil
}

func (c *Client) GetNode(ctx context.Context, id string) (*Node, error) {
	var n Node
	if err := c.do(ctx, http.MethodGet, "/api/nodes/"+id, nil, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// NodeUpdate 节点可编辑字段（指针字段 = 仅更新非 nil 项，与 controller 对齐）
type NodeUpdate struct {
	IP              *string  `json:"ip"`
	CoreNum         *int     `json:"core_num"`
	CoreFrequency   *float64 `json:"core_frequency"`
	MemorySize      *int64   `json:"memory_size"`
	StorageSize     *int64   `json:"storage_size"`
	Location        *string  `json:"location"`
	ServiceProvider *string  `json:"service_provider"`
	NetRxLimitMbps  *int     `json:"net_rx_limit_mbps"`
	NetTxLimitMbps  *int     `json:"net_tx_limit_mbps"`
}

// UpdateNode 更新节点配置（非 nil 字段生效）
func (c *Client) UpdateNode(ctx context.Context, id string, u NodeUpdate) (*Node, error) {
	var n Node
	if err := c.do(ctx, http.MethodPut, "/api/nodes/"+id, u, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// DeleteNode 删除节点（controller 对被 node_agent 引用的节点返回 409）
func (c *Client) DeleteNode(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/nodes/"+id, nil, nil)
}

// ObserveForward 转发调度观测请求到 controller /api/observe/*（管理员观测，走 admin 鉴权）
// query 为原始 query string（如 "hours=24&limit=100"），透传给 controller（空串不附加）。
func (c *Client) ObserveForward(ctx context.Context, method, subpath, query string, body, out any) error {
	fullPath := "/api/observe" + subpath
	if query != "" {
		fullPath += "?" + query
	}
	return c.do(ctx, method, fullPath, body, out)
}

// ---------------------------------------------------------------------------
// NodeAgent（管理员管理）
// ---------------------------------------------------------------------------

type NodeAgent struct {
	ID              string  `json:"ID"`
	NodeId          string  `json:"NodeId"`
	Port            int32   `json:"Port"`
	Status          int32   `json:"Status"` // 0=Disabled 1=Enabled
	Alive           bool    `json:"Alive"`             // 存活检测（controller 心跳探测）
	LastHeartbeatAt *string `json:"LastHeartbeatAt"`
}

func (c *Client) CreateNodeAgent(ctx context.Context, name, nodeID string, port int32) (*NodeAgent, error) {
	var a NodeAgent
	body := map[string]any{"name": name, "node_id": nodeID}
	if port > 0 {
		body["port"] = port
	}
	if err := c.do(ctx, http.MethodPost, "/api/node-agents", body, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (c *Client) ListNodeAgents(ctx context.Context) ([]*NodeAgent, error) {
	var out struct {
		NodeAgents []*NodeAgent `json:"node_agents"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/node-agents", nil, &out); err != nil {
		return nil, err
	}
	return out.NodeAgents, nil
}

func (c *Client) SetNodeAgentEnabled(ctx context.Context, id string, enabled bool) (*NodeAgent, error) {
	var a NodeAgent
	action := "disable"
	if enabled {
		action = "enable"
	}
	if err := c.do(ctx, http.MethodPost, "/api/node-agents/"+id+"/"+action, nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// ---------------------------------------------------------------------------
// Game（管理员管理，写操作同步 asset_service）
// ---------------------------------------------------------------------------

type Game struct {
	ID                string `json:"ID"`
	Name              string `json:"Name"`
	AppId             string `json:"AppId"`
	ContainerConfigID string `json:"ContainerConfigID"`
}

func (c *Client) CreateGame(ctx context.Context, name, appID string) (*Game, error) {
	var g Game
	if err := c.do(ctx, http.MethodPost, "/api/games", map[string]string{"name": name, "app_id": appID}, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (c *Client) ListGames(ctx context.Context) ([]*Game, error) {
	var out struct {
		Games []*Game `json:"games"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/games", nil, &out); err != nil {
		return nil, err
	}
	return out.Games, nil
}

func (c *Client) GetGame(ctx context.Context, id string) (*Game, error) {
	var g Game
	if err := c.do(ctx, http.MethodGet, "/api/games/"+id, nil, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (c *Client) UpdateGame(ctx context.Context, id, name, appID string) (*Game, error) {
	var g Game
	if err := c.do(ctx, http.MethodPut, "/api/games/"+id, map[string]string{"name": name, "app_id": appID}, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (c *Client) DeleteGame(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/games/"+id, nil, nil)
}

// ---------------------------------------------------------------------------
// GameBuild（管理员管理，资产版本）
// ---------------------------------------------------------------------------

// GameBuild asset_service 的构建版本（JSON 字段为 snake_case，与 proto json tag 一致）
type GameBuild struct {
	BuildId           string         `json:"build_id"`
	Game              *struct{ Id string `json:"id"` } `json:"game,omitempty"`
	Channel           *string        `json:"channel,omitempty"`
	AdapterId         string         `json:"adapter_id,omitempty"`
	AdapterVersion    *string        `json:"adapter_version,omitempty"`
	UpstreamVersion   *string        `json:"upstream_version,omitempty"`
	ArtifactUri       *string        `json:"artifact_uri,omitempty"`
	ArtifactImageName *string        `json:"artifact_image_name,omitempty"`
	ArtifactImageTag  *string        `json:"artifact_image_tag,omitempty"`
	Status            int32          `json:"status,omitempty"`
	CreatedAt         string         `json:"created_at,omitempty"`
	UpdatedAt         string         `json:"updated_at,omitempty"`
	// M5：配置 schema / 适配器元数据（gen_manifest.py 产物），随构建注册携带
	SchemaJson        *string        `json:"schema_json,omitempty"`
	AdapterMetadata   map[string]any `json:"adapter_metadata,omitempty"`
}

func (c *Client) ListGameBuilds(ctx context.Context, gameID, channel string) ([]*GameBuild, error) {
	var out struct {
		Builds []*GameBuild `json:"builds"`
	}
	path := "/api/games/" + gameID + "/builds"
	if channel != "" {
		path += "?channel=" + channel
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Builds, nil
}

// RegisterGameBuild 注册新构建（增量迭代语义）。controller 的注册接口是平铺字段
// （game_id + 可选覆盖字段），build_id 由系统按 {game_id}-{channel}-{tag} 生成；
// base_build_id 指定迭代基准（缺省 = 同 channel 最新 Available）。
// schema_json / adapter_metadata 可选（gen_manifest.py 产物），不提供则从 base 继承。
func (c *Client) RegisterGameBuild(ctx context.Context, gameID string, build *GameBuild, baseBuildID string) (*GameBuild, error) {
	body := map[string]any{
		"game_id":             gameID,
		"channel":             derefStr(build.Channel),
		"adapter_id":          build.AdapterId,
		"adapter_version":     derefStr(build.AdapterVersion),
		"upstream_version":    derefStr(build.UpstreamVersion),
		"artifact_uri":        derefStr(build.ArtifactUri),
		"artifact_image_name": derefStr(build.ArtifactImageName),
		"artifact_image_tag":  derefStr(build.ArtifactImageTag),
	}
	if baseBuildID != "" {
		body["base_build_id"] = baseBuildID
	}
	if build.SchemaJson != nil && *build.SchemaJson != "" {
		body["schema_json"] = *build.SchemaJson
	}
	if build.AdapterMetadata != nil {
		body["adapter_metadata"] = build.AdapterMetadata
	}
	var out GameBuild
	if err := c.do(ctx, http.MethodPost, "/api/games/"+gameID+"/builds", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (c *Client) GetGameBuild(ctx context.Context, gameID, buildID string) (*GameBuild, error) {
	var out GameBuild
	if err := c.do(ctx, http.MethodGet, "/api/games/"+gameID+"/builds/"+buildID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// SteamBranch（管理员管理）
// ---------------------------------------------------------------------------

type SteamBranch struct {
	Id          string `json:"Id"`
	BranchName  string `json:"BranchName"`
	LastBuildId uint64 `json:"LastBuildId"`
	Description string `json:"Description"`
	GameId      string `json:"GameId"`
	Status      int32  `json:"Status"` // 0=Disable 1=Enable 2=Abandoned
	CreateTime  string `json:"CreateTime"`
	UpdateTime  string `json:"UpdateTime"`
}

func (c *Client) ListBranches(ctx context.Context, gameID string) ([]*SteamBranch, error) {
	var out struct {
		Branches []*SteamBranch `json:"branches"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/games/"+gameID+"/branches", nil, &out); err != nil {
		return nil, err
	}
	return out.Branches, nil
}

// SyncBranches 手动触发分支同步（从 asset_service 拉取）
func (c *Client) SyncBranches(ctx context.Context, gameID string) error {
	return c.do(ctx, http.MethodPost, "/api/games/"+gameID+"/branches/sync", nil, nil)
}

// UpdateBranchCache 在指定 node_agent 上触发分支缓存下载/更新
func (c *Client) UpdateBranchCache(ctx context.Context, gameID, branchName, nodeAgentID string) error {
	return c.do(ctx, http.MethodPost, "/api/games/"+gameID+"/branches/"+branchName+"/cache",
		map[string]string{"node_agent_id": nodeAgentID}, nil)
}

// do 发送请求；out 非空时把 2xx 响应 JSON 解码到 out
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request controller %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("controller %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		}
		return err
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}