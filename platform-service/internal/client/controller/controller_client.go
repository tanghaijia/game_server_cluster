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

// CreateGameInstance 在 controller 上创建实例（初始 stopped）
func (c *Client) CreateGameInstance(ctx context.Context, gameID, buildID string) (*GameInstance, error) {
	body := map[string]string{"game_id": gameID}
	if buildID != "" {
		body["game_build_id"] = buildID
	}
	var inst GameInstance
	if err := c.do(ctx, http.MethodPost, "/api/game-instances", body, &inst); err != nil {
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

// ---------------------------------------------------------------------------
// NodeAgent（管理员管理）
// ---------------------------------------------------------------------------

type NodeAgent struct {
	ID     string `json:"ID"`
	NodeId string `json:"NodeId"`
	Port   int32  `json:"Port"`
	Status int32  `json:"Status"` // 0=Disabled 1=Enabled
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
