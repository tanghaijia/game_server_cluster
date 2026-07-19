package assetservice

import (
	"context"
	"fmt"

	assetservicev1 "controller-go/internal/third/assetservice/v1"
	"google.golang.org/grpc"
)

// AssetServiceFaceClient 封装 protobuf 生成的 AssetServiceClient，
// 为业务层提供稳定的调用入口，隔离底层 gRPC 实现的变更。
type AssetServiceFaceClient struct {
	client assetservicev1.AssetServiceClient
}

// NewAssetServiceFaceClient 创建封装客户端。
// cc 由调用方管理生命周期，例如从 *grpc.ClientConn 传入。
func NewAssetServiceFaceClient(cc grpc.ClientConnInterface) *AssetServiceFaceClient {
	return &AssetServiceFaceClient{
		client: assetservicev1.NewAssetServiceClient(cc),
	}
}

// ---------------------------------------------------------------------------
// GameBuild 领域
// ---------------------------------------------------------------------------

func (c *AssetServiceFaceClient) ResolveGameBuild(ctx context.Context, in *assetservicev1.ResolveGameBuildRequest, opts ...grpc.CallOption) (*assetservicev1.ResolveGameBuildResponse, error) {
	out, err := c.client.ResolveGameBuild(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("resolve game build: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) RegisterGameBuild(ctx context.Context, in *assetservicev1.RegisterGameBuildRequest, opts ...grpc.CallOption) (*assetservicev1.RegisterGameBuildResponse, error) {
	out, err := c.client.RegisterGameBuild(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("register game build: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetGameBuild(ctx context.Context, in *assetservicev1.GetGameBuildRequest, opts ...grpc.CallOption) (*assetservicev1.GetGameBuildResponse, error) {
	out, err := c.client.GetGameBuild(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get game build: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Snapshot 领域
// ---------------------------------------------------------------------------

func (c *AssetServiceFaceClient) CreateSnapshot(ctx context.Context, in *assetservicev1.CreateSnapshotRequest, opts ...grpc.CallOption) (*assetservicev1.CreateSnapshotResponse, error) {
	out, err := c.client.CreateSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("create snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) CompleteSnapshot(ctx context.Context, in *assetservicev1.CompleteSnapshotRequest, opts ...grpc.CallOption) (*assetservicev1.CompleteSnapshotResponse, error) {
	out, err := c.client.CompleteSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("complete snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) FailSnapshot(ctx context.Context, in *assetservicev1.FailSnapshotRequest, opts ...grpc.CallOption) (*assetservicev1.FailSnapshotResponse, error) {
	out, err := c.client.FailSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("fail snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetSnapshot(ctx context.Context, in *assetservicev1.GetSnapshotRequest, opts ...grpc.CallOption) (*assetservicev1.GetSnapshotResponse, error) {
	out, err := c.client.GetSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetLatestSnapshot(ctx context.Context, in *assetservicev1.GetLatestSnapshotRequest, opts ...grpc.CallOption) (*assetservicev1.GetLatestSnapshotResponse, error) {
	out, err := c.client.GetLatestSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get latest snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) SetLatestSnapshot(ctx context.Context, in *assetservicev1.SetLatestSnapshotRequest, opts ...grpc.CallOption) (*assetservicev1.SetLatestSnapshotResponse, error) {
	out, err := c.client.SetLatestSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("set latest snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetSnapshotRestorePlan(ctx context.Context, in *assetservicev1.GetSnapshotRestorePlanRequest, opts ...grpc.CallOption) (*assetservicev1.GetSnapshotRestorePlanResponse, error) {
	out, err := c.client.GetSnapshotRestorePlan(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get snapshot restore plan: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) ListSnapshots(ctx context.Context, in *assetservicev1.ListSnapshotsRequest, opts ...grpc.CallOption) (*assetservicev1.ListSnapshotsResponse, error) {
	out, err := c.client.ListSnapshots(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// ModManifest 领域
// ---------------------------------------------------------------------------

func (c *AssetServiceFaceClient) RegisterModManifest(ctx context.Context, in *assetservicev1.RegisterModManifestRequest, opts ...grpc.CallOption) (*assetservicev1.RegisterModManifestResponse, error) {
	out, err := c.client.RegisterModManifest(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("register mod manifest: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetModManifest(ctx context.Context, in *assetservicev1.GetModManifestRequest, opts ...grpc.CallOption) (*assetservicev1.GetModManifestResponse, error) {
	out, err := c.client.GetModManifest(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get mod manifest: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) CheckBuildModCompatibility(ctx context.Context, in *assetservicev1.CheckBuildModCompatibilityRequest, opts ...grpc.CallOption) (*assetservicev1.CheckBuildModCompatibilityResponse, error) {
	out, err := c.client.CheckBuildModCompatibility(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("check build mod compatibility: %w", err)
	}
	return out, nil
}
