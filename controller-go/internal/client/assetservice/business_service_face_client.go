package assetservice

import (
	"context"
	"fmt"

	assetservicev1 "controller-go/internal/third/assetservice/v1"
	"google.golang.org/grpc"
)

// BusinessServiceFaceClient 封装 protobuf 生成的 BusinessServiceClient，
// 为业务层提供稳定的调用入口，隔离底层 gRPC 实现的变更。
type BusinessServiceFaceClient struct {
	client assetservicev1.BusinessServiceClient
}

// NewBusinessServiceFaceClient 创建封装客户端。
// cc 由调用方管理生命周期，例如从 *grpc.ClientConn 传入。
func NewBusinessServiceFaceClient(cc grpc.ClientConnInterface) *BusinessServiceFaceClient {
	return &BusinessServiceFaceClient{
		client: assetservicev1.NewBusinessServiceClient(cc),
	}
}

// ---------------------------------------------------------------------------
// Game 领域
// ---------------------------------------------------------------------------

func (c *BusinessServiceFaceClient) CreateGame(ctx context.Context, in *assetservicev1.CreateGameRequest, opts ...grpc.CallOption) (*assetservicev1.CreateGameResponse, error) {
	out, err := c.client.CreateGame(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("create game: %w", err)
	}
	return out, nil
}

func (c *BusinessServiceFaceClient) GetGame(ctx context.Context, in *assetservicev1.GetGameRequest, opts ...grpc.CallOption) (*assetservicev1.GetGameResponse, error) {
	out, err := c.client.GetGame(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get game: %w", err)
	}
	return out, nil
}

func (c *BusinessServiceFaceClient) UpdateGame(ctx context.Context, in *assetservicev1.UpdateGameRequest, opts ...grpc.CallOption) (*assetservicev1.UpdateGameResponse, error) {
	out, err := c.client.UpdateGame(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("update game: %w", err)
	}
	return out, nil
}

func (c *BusinessServiceFaceClient) DeleteGame(ctx context.Context, in *assetservicev1.DeleteGameRequest, opts ...grpc.CallOption) (*assetservicev1.DeleteGameResponse, error) {
	out, err := c.client.DeleteGame(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("delete game: %w", err)
	}
	return out, nil
}

func (c *BusinessServiceFaceClient) ListGames(ctx context.Context, in *assetservicev1.ListGamesRequest, opts ...grpc.CallOption) (*assetservicev1.ListGamesResponse, error) {
	out, err := c.client.ListGames(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// SteamBranch 领域
// ---------------------------------------------------------------------------

func (c *BusinessServiceFaceClient) ListSteamBranches(ctx context.Context, in *assetservicev1.ListSteamBranchesRequest, opts ...grpc.CallOption) (*assetservicev1.ListSteamBranchesResponse, error) {
	out, err := c.client.ListSteamBranches(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("list steam branches: %w", err)
	}
	return out, nil
}
