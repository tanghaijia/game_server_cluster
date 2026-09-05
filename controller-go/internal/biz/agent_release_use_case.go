package biz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"controller-go/internal/client/assetservice"
	"controller-go/internal/entity"
	"controller-go/internal/repository"

	assetservicev1 "controller-go/internal/third/assetservice/v1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"gorm.io/gorm"
)

// versionRe 版本号格式约束：v 开头 + 数字点段（防注入/混淆），如 v0.1.1
var versionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$`)

var (
	ErrReleaseInvalidVersion = errors.New("invalid version (expect vX.Y.Z)")
	ErrReleaseInvalidTarget  = errors.New("os/arch 不支持或格式错误")
	ErrReleaseNotFound       = errors.New("release not found")
)

// uploadChunkSize 上传分块大小（1 MiB），经 gRPC 客户端流转发给 asset_service。
const uploadChunkSize = 1 << 20

// AgentReleaseUploader 上传通道抽象：*assetservice.AssetServiceFaceClient 满足；
// 测试用 fake 实现（不依赖真实 gRPC 连接）。
type AgentReleaseUploader interface {
	PutAgentRelease(ctx context.Context, opts ...grpc.CallOption) (assetservice.AgentReleaseUploadStream, error)
}

// AgentReleaseUseCase node_agent 发布版本管理。
//
// P2（docs/agent-release-asset-service-redesign.md）：二进制本体由 asset_service 接收并写入
// 对象存储（S3/MinIO），controller 只登记清单 —— storage_key = 对象键 object_key，bucket 落库，
// controller 不再本地落盘、不再提供下载端点。
type AgentReleaseUseCase struct {
	repo     repository.AgentReleaseRepository
	uploader AgentReleaseUploader // → asset_service PutAgentRelease 流
}

func NewAgentReleaseUseCase(repo repository.AgentReleaseRepository, uploader AgentReleaseUploader) *AgentReleaseUseCase {
	return &AgentReleaseUseCase{repo: repo, uploader: uploader}
}

// RegisterParams 上传登记参数
type RegisterParams struct {
	Version string // vX.Y.Z
	OS      string // linux / windows
	Arch    string // amd64 / arm64
	Note    string
	ByUser  string
	Body    io.Reader // 二进制内容
}

var allowedOS = map[string]bool{"linux": true, "windows": true}
var allowedArch = map[string]bool{"amd64": true, "arm64": true}

// Register 校验参数 → 流式上传 asset_service（边传边由对方算 sha256）→ 登记清单。
// 重复版本+平台：409。落库失败时对象已写（无删除接口），记日志由后续清理。
func (uc *AgentReleaseUseCase) Register(ctx context.Context, p RegisterParams) (*entity.AgentRelease, error) {
	if !versionRe.MatchString(p.Version) {
		return nil, ErrReleaseInvalidVersion
	}
	osName := strings.ToLower(strings.TrimSpace(p.OS))
	arch := strings.ToLower(strings.TrimSpace(p.Arch))
	if !allowedOS[osName] || !allowedArch[arch] {
		return nil, ErrReleaseInvalidTarget
	}
	if p.Body == nil {
		return nil, errors.New("release body is required")
	}
	if existing, err := uc.repo.GetByVersionOSArch(ctx, p.Version, osName, arch); err == nil && existing != nil {
		return nil, fmt.Errorf("release %s %s/%s 已存在（id=%s），如需覆盖请先删除", p.Version, osName, arch, existing.ID)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	stream, err := uc.uploader.PutAgentRelease(ctx)
	if err != nil {
		return nil, fmt.Errorf("open release upload: %w", err)
	}
	// 首条带元数据（asset_service 以首条 version/os/arch 为准）
	if err := stream.Send(&assetservicev1.PutAgentReleaseRequest{
		Version: p.Version, Os: osName, Arch: arch,
	}); err != nil {
		return nil, fmt.Errorf("send release header: %w", err)
	}
	buf := make([]byte, uploadChunkSize)
	for {
		n, readErr := p.Body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if err := stream.Send(&assetservicev1.PutAgentReleaseRequest{
				Version: p.Version, Os: osName, Arch: arch, Chunk: chunk,
			}); err != nil {
				return nil, fmt.Errorf("send release chunk: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read upload body: %w", readErr)
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("asset_service 接收 release 失败: %w", err)
	}

	release := &entity.AgentRelease{
		ID:         uuid.NewString(),
		Version:    p.Version,
		OS:         osName,
		Arch:       arch,
		SHA256:     resp.GetSha256(),
		SizeBytes:  int64(resp.GetSizeBytes()),
		Bucket:     resp.GetBucket(),
		StorageKey: resp.GetObjectKey(),
		Note:       p.Note,
		CreatedBy:  p.ByUser,
	}
	if release.SHA256 == "" || release.StorageKey == "" {
		return nil, errors.New("asset_service 返回缺 sha256/object_key")
	}
	if err := uc.repo.Save(ctx, release); err != nil {
		// 对象已写入对象存储；无删除接口，先记录（孤儿由后续清理/覆盖发布处理）
		return nil, err
	}
	return release, nil
}

// List 发布清单（按时间倒序）
func (uc *AgentReleaseUseCase) List(ctx context.Context) ([]*entity.AgentRelease, error) {
	return uc.repo.ListAll(ctx)
}

// Get 详情
func (uc *AgentReleaseUseCase) Get(ctx context.Context, id string) (*entity.AgentRelease, error) {
	release, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReleaseNotFound
		}
		return nil, err
	}
	return release, nil
}
