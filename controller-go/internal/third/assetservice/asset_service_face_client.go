package assetservicev1

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
)

// AssetServiceFaceClient 封装 protobuf 生成的 AssetServiceClient，
// 为业务层提供稳定的调用入口，隔离底层 gRPC 实现的变更。
type AssetServiceFaceClient struct {
	client AssetServiceClient
}

// NewAssetServiceFaceClient 创建封装客户端。
// cc 由调用方管理生命周期，例如从 *grpc.ClientConn 传入。
func NewAssetServiceFaceClient(cc grpc.ClientConnInterface) *AssetServiceFaceClient {
	return &AssetServiceFaceClient{
		client: NewAssetServiceClient(cc),
	}
}

// ---------------------------------------------------------------------------
// GameBuild 领域
// ---------------------------------------------------------------------------

func (c *AssetServiceFaceClient) ResolveGameBuild(ctx context.Context, in *ResolveGameBuildRequest, opts ...grpc.CallOption) (*ResolveGameBuildResponse, error) {
	out, err := c.client.ResolveGameBuild(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("resolve game build: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) RegisterGameBuild(ctx context.Context, in *RegisterGameBuildRequest, opts ...grpc.CallOption) (*RegisterGameBuildResponse, error) {
	out, err := c.client.RegisterGameBuild(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("register game build: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetGameBuild(ctx context.Context, in *GetGameBuildRequest, opts ...grpc.CallOption) (*GetGameBuildResponse, error) {
	out, err := c.client.GetGameBuild(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get game build: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Snapshot 领域
// ---------------------------------------------------------------------------

func (c *AssetServiceFaceClient) CreateSnapshot(ctx context.Context, in *CreateSnapshotRequest, opts ...grpc.CallOption) (*CreateSnapshotResponse, error) {
	out, err := c.client.CreateSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("create snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) CompleteSnapshot(ctx context.Context, in *CompleteSnapshotRequest, opts ...grpc.CallOption) (*CompleteSnapshotResponse, error) {
	out, err := c.client.CompleteSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("complete snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) FailSnapshot(ctx context.Context, in *FailSnapshotRequest, opts ...grpc.CallOption) (*FailSnapshotResponse, error) {
	out, err := c.client.FailSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("fail snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetSnapshot(ctx context.Context, in *GetSnapshotRequest, opts ...grpc.CallOption) (*GetSnapshotResponse, error) {
	out, err := c.client.GetSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetLatestSnapshot(ctx context.Context, in *GetLatestSnapshotRequest, opts ...grpc.CallOption) (*GetLatestSnapshotResponse, error) {
	out, err := c.client.GetLatestSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get latest snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) SetLatestSnapshot(ctx context.Context, in *SetLatestSnapshotRequest, opts ...grpc.CallOption) (*SetLatestSnapshotResponse, error) {
	out, err := c.client.SetLatestSnapshot(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("set latest snapshot: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetSnapshotRestorePlan(ctx context.Context, in *GetSnapshotRestorePlanRequest, opts ...grpc.CallOption) (*GetSnapshotRestorePlanResponse, error) {
	out, err := c.client.GetSnapshotRestorePlan(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get snapshot restore plan: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) ListSnapshots(ctx context.Context, in *ListSnapshotsRequest, opts ...grpc.CallOption) (*ListSnapshotsResponse, error) {
	out, err := c.client.ListSnapshots(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// ModManifest 领域
// ---------------------------------------------------------------------------

func (c *AssetServiceFaceClient) RegisterModManifest(ctx context.Context, in *RegisterModManifestRequest, opts ...grpc.CallOption) (*RegisterModManifestResponse, error) {
	out, err := c.client.RegisterModManifest(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("register mod manifest: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) GetModManifest(ctx context.Context, in *GetModManifestRequest, opts ...grpc.CallOption) (*GetModManifestResponse, error) {
	out, err := c.client.GetModManifest(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("get mod manifest: %w", err)
	}
	return out, nil
}

func (c *AssetServiceFaceClient) CheckBuildModCompatibility(ctx context.Context, in *CheckBuildModCompatibilityRequest, opts ...grpc.CallOption) (*CheckBuildModCompatibilityResponse, error) {
	out, err := c.client.CheckBuildModCompatibility(ctx, in, opts...)
	if err != nil {
		return nil, fmt.Errorf("check build mod compatibility: %w", err)
	}
	return out, nil
}
