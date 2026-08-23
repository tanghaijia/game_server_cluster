package biz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"

	assetservicev1 "controller-go/internal/third/assetservice/v1"
	"controller-go/internal/client/assetservice"

	"github.com/google/uuid"
)

// ErrCredentialPoolEmpty 凭证池为空（required 凭证无法分配 → 实例启动失败）
var ErrCredentialPoolEmpty = errors.New("credential pool is empty, please ask admin to add credentials")

// CredentialUseCase 外部受限凭证池（M8，§3.6.5）。
// 平台侧全通用：按 game_id + resource_type 池化，生命周期挂钩由 ReconcileDispatcher 调用
// （StatusStarting 分配 / onCleanInstanceSucceeded 释放 / Failed 释放）。
type CredentialUseCase struct {
	repo        repository.CredentialPoolRepository
	assetClient *assetservice.AssetServiceFaceClient
}

func NewCredentialUseCase(repo repository.CredentialPoolRepository, assetClient *assetservice.AssetServiceFaceClient) *CredentialUseCase {
	return &CredentialUseCase{repo: repo, assetClient: assetClient}
}

// DeclaredTypes 该游戏已注册 build 声明过的凭证类型（adapter.toml [[credentials]].pool 去重）。
// resource_type 是枚举而非自由输入：只允许这里返回的类型。
func (uc *CredentialUseCase) DeclaredTypes(ctx context.Context, gameID string) ([]string, error) {
	if uc.assetClient == nil {
		return nil, nil
	}
	resp, err := uc.assetClient.ListGameBuilds(ctx, &assetservicev1.ListGameBuildsRequest{GameId: gameID})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var out []string
	for _, b := range resp.GetBuilds() {
		for _, spec := range b.GetAdapterMetadata().GetCredentials() {
			pool := spec.GetPool()
			if pool == "" {
				continue
			}
			if _, ok := seen[pool]; ok {
				continue
			}
			seen[pool] = struct{}{}
			out = append(out, pool)
		}
	}
	return out, nil
}

// Create 批量录入凭证（admin 从官网创建后粘贴）。
// resource_type 必须是该游戏已声明（adapter.toml [[credentials]].pool）的类型，
// 防止拼写错误导致录了用不上。
func (uc *CredentialUseCase) Create(ctx context.Context, gameID, resourceType string, secrets []string, remark string) (int, error) {
	declared, err := uc.DeclaredTypes(ctx, gameID)
	if err != nil {
		return 0, err
	}
	ok := false
	for _, t := range declared {
		if t == resourceType {
			ok = true
			break
		}
	}
	if !ok {
		if len(declared) == 0 {
			return 0, fmt.Errorf("该游戏尚未注册任何带凭证声明的构建（adapter.toml [[credentials]]），无法录入凭证")
		}
		return 0, fmt.Errorf("无效的凭证类型 %q，可选类型: %v（须与该游戏构建的 adapter.toml [[credentials]].pool 一致）", resourceType, declared)
	}
	now := time.Now()
	creds := make([]entity.CredentialPool, 0, len(secrets))
	for _, s := range secrets {
		s = trimSpace(s)
		if s == "" {
			continue
		}
		creds = append(creds, entity.CredentialPool{
			ID:           uuid.NewString(),
			GameID:       gameID,
			ResourceType: resourceType,
			Secret:       s,
			Status:       entity.CredentialAvailable,
			Remark:       remark,
			CreateTime:   now,
			UpdateTime:   now,
		})
	}
	if len(creds) == 0 {
		return 0, errors.New("no valid secrets provided")
	}
	if err := uc.repo.Create(ctx, creds); err != nil {
		return 0, err
	}
	return len(creds), nil
}

// List 列出某游戏凭证（secret 脱敏展示，完整值不返回）
func (uc *CredentialUseCase) List(ctx context.Context, gameID, resourceType string) ([]CredentialView, error) {
	rows, err := uc.repo.ListByGame(ctx, gameID, resourceType)
	if err != nil {
		return nil, err
	}
	out := make([]CredentialView, 0, len(rows))
	for _, r := range rows {
		out = append(out, CredentialView{
			ID:             r.ID,
			GameID:         r.GameID,
			ResourceType:   r.ResourceType,
			SecretMasked:   maskSecret(r.Secret),
			Status:         r.Status,
			InstanceID:     derefStr(r.InstanceID),
			LastInstanceID: derefStr(r.LastInstanceID),
			AllocatedAt:    r.AllocatedAt,
			ReleasedAt:     r.ReleasedAt,
			Remark:         r.Remark,
			CreateTime:     r.CreateTime,
		})
	}
	return out, nil
}

// Delete 删除凭证（in_use 拒绝）
func (uc *CredentialUseCase) Delete(ctx context.Context, id string) error {
	cred, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if cred == nil {
		return errors.New("credential not found")
	}
	if cred.Status == entity.CredentialInUse {
		return errors.New("credential is in use by an instance, cannot delete")
	}
	return uc.repo.Delete(ctx, id)
}

// ForceRelease 强制释放（orphan 或任意状态 → available）
func (uc *CredentialUseCase) ForceRelease(ctx context.Context, id string) error {
	cred, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if cred == nil {
		return errors.New("credential not found")
	}
	return uc.repo.ForceRelease(ctx, id)
}

// AllocateForInstance 为实例分配凭证（幂等）：
//  1. 该实例已有 in_use 凭证 → 直接复用（reconcile 重入安全）；
//  2. 优先复用上次占用者（last_instance_id）仍 available 的凭证（重启拿回原 token）；
//  3. 否则取最早录入的 available 凭证（FIFO）；
//  4. 池空 → ErrCredentialPoolEmpty。
func (uc *CredentialUseCase) AllocateForInstance(ctx context.Context, gameID, resourceType, instanceID string) (string, error) {
	// 幂等：已有占用直接返回
	allocated, err := uc.repo.FindAllocatedByInstance(ctx, gameID, resourceType, instanceID)
	if err != nil {
		return "", err
	}
	if len(allocated) > 0 {
		return allocated[0].Secret, nil
	}

	rows, err := uc.repo.ListByGame(ctx, gameID, resourceType)
	if err != nil {
		return "", err
	}
	// 优先复用上次占用者
	for _, r := range rows {
		if r.Status == entity.CredentialAvailable && derefStr(r.LastInstanceID) == instanceID {
			ok, err := uc.repo.Acquire(ctx, r.ID, instanceID)
			if err != nil {
				return "", err
			}
			if ok {
				return r.Secret, nil
			}
		}
	}
	// FIFO：最早录入的 available
	for _, r := range rows {
		if r.Status == entity.CredentialAvailable {
			ok, err := uc.repo.Acquire(ctx, r.ID, instanceID)
			if err != nil {
				return "", err
			}
			if ok {
				return r.Secret, nil
			}
		}
	}
	return "", fmt.Errorf("%w (game=%s pool=%s)", ErrCredentialPoolEmpty, gameID, resourceType)
}

// ReleaseByInstance 释放实例占用的全部凭证（幂等；停止/失败路径调用）
func (uc *CredentialUseCase) ReleaseByInstance(ctx context.Context, instanceID string) error {
	return uc.repo.ReleaseByInstance(ctx, instanceID)
}

// CredentialView 凭证列表视图（secret 脱敏）
type CredentialView struct {
	ID             string     `json:"id"`
	GameID         string     `json:"game_id"`
	ResourceType   string     `json:"resource_type"`
	SecretMasked   string     `json:"secret_masked"`
	Status         string     `json:"status"`
	InstanceID     string     `json:"instance_id"`
	LastInstanceID string     `json:"last_instance_id"`
	AllocatedAt    *time.Time `json:"allocated_at"`
	ReleasedAt     *time.Time `json:"released_at"`
	Remark         string     `json:"remark"`
	CreateTime     time.Time  `json:"create_time"`
}

// maskSecret 脱敏：保留前 6 与后 4 字符，中间打码
func maskSecret(s string) string {
	runes := []rune(s)
	n := len(runes)
	if n <= 10 {
		return "****"
	}
	return string(runes[:6]) + "****" + string(runes[n-4:])
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}
