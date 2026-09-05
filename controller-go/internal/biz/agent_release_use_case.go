package biz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"controller-go/internal/entity"
	"controller-go/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// versionRe 版本号格式约束：v 开头 + 数字点段（防注入/混淆），如 v0.1.1
var versionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$`)

var (
	ErrReleaseInvalidVersion = errors.New("invalid version (expect vX.Y.Z)")
	ErrReleaseInvalidTarget  = errors.New("os/arch 不支持或格式错误")
	ErrReleaseNotFound       = errors.New("release not found")
)

// AgentReleaseUseCase node_agent 发布版本管理（P1，docs/node-agent-upgrade-design.md §3.1）
type AgentReleaseUseCase struct {
	repo   repository.AgentReleaseRepository
	store  ReleaseStore
	byUser string // 上传者标识（admin 用户名，可空）
}

func NewAgentReleaseUseCase(repo repository.AgentReleaseRepository, store ReleaseStore) *AgentReleaseUseCase {
	return &AgentReleaseUseCase{repo: repo, store: store}
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

// Register 校验参数 → 落 ReleaseStore → 登记清单（重复版本+平台：409）
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

	id := uuid.NewString()
	key := fmt.Sprintf("agent-release-%s-%s-%s", p.Version, osName, arch)
	storageKey, sha, size, err := uc.store.Put(key, p.Body)
	if err != nil {
		return nil, fmt.Errorf("store release: %w", err)
	}
	release := &entity.AgentRelease{
		ID:         id,
		Version:    p.Version,
		OS:         osName,
		Arch:       arch,
		SHA256:     sha,
		SizeBytes:  size,
		StorageKey: storageKey,
		Note:       p.Note,
		CreatedBy:  p.ByUser,
	}
	if err := uc.repo.Save(ctx, release); err != nil {
		// 落库失败回收文件，避免孤儿
		_ = uc.store.Delete(storageKey)
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

// OpenBinary 打开二进制流（下载）
func (uc *AgentReleaseUseCase) OpenBinary(ctx context.Context, id string) (*entity.AgentRelease, io.ReadCloser, error) {
	release, err := uc.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	rc, err := uc.store.Open(release.StorageKey)
	if err != nil {
		return nil, nil, fmt.Errorf("open release binary: %w", err)
	}
	return release, rc, nil
}
