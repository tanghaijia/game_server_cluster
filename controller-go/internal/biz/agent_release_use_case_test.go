package biz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"controller-go/internal/client/assetservice"
	"controller-go/internal/entity"

	assetservicev1 "controller-go/internal/third/assetservice/v1"
	"google.golang.org/grpc"
	"gorm.io/gorm"
)

// fakeAgentReleaseRepo 内存实现（避免依赖 DB）
type fakeAgentReleaseRepo struct {
	byID map[string]*entity.AgentRelease
	all  []*entity.AgentRelease
}

func newFakeAgentReleaseRepo() *fakeAgentReleaseRepo {
	return &fakeAgentReleaseRepo{byID: map[string]*entity.AgentRelease{}}
}

func (f *fakeAgentReleaseRepo) Save(_ context.Context, r *entity.AgentRelease) error {
	f.byID[r.ID] = r
	// ListAll 按时间倒序语义简化：新插入放前面
	f.all = append([]*entity.AgentRelease{r}, f.all...)
	return nil
}
func (f *fakeAgentReleaseRepo) GetByID(_ context.Context, id string) (*entity.AgentRelease, error) {
	if r, ok := f.byID[id]; ok {
		return r, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (f *fakeAgentReleaseRepo) ListAll(_ context.Context) ([]*entity.AgentRelease, error) { return f.all, nil }
func (f *fakeAgentReleaseRepo) GetByVersionOSArch(_ context.Context, version, osName, arch string) (*entity.AgentRelease, error) {
	for _, r := range f.all {
		if r.Version == version && r.OS == osName && r.Arch == arch {
			return r, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// ---- fake uploader：模拟 asset_service PutAgentRelease 流（收集分块并算 sha256） ----

type fakeReleaseUploader struct {
	chunks [][]byte
	err    error // CloseAndRecv 时返回的错误（可注入）
}

type fakeUploadStream struct {
	u *fakeReleaseUploader
}

func (s *fakeUploadStream) Send(req *assetservicev1.PutAgentReleaseRequest) error {
	if len(req.Chunk) > 0 {
		s.u.chunks = append(s.u.chunks, append([]byte{}, req.Chunk...))
	}
	return nil
}

func (s *fakeUploadStream) CloseAndRecv() (*assetservicev1.PutAgentReleaseResponse, error) {
	if s.u.err != nil {
		return nil, s.u.err
	}
	h := sha256.New()
	var total uint64
	for _, c := range s.u.chunks {
		h.Write(c)
		total += uint64(len(c))
	}
	return &assetservicev1.PutAgentReleaseResponse{
		Bucket:    "cluster",
		ObjectKey: "agent-release/test/node-agent",
		Sha256:    hex.EncodeToString(h.Sum(nil)),
		SizeBytes: total,
	}, nil
}

func (f *fakeReleaseUploader) PutAgentRelease(_ context.Context, _ ...grpc.CallOption) (assetservice.AgentReleaseUploadStream, error) {
	return &fakeUploadStream{u: f}, nil
}

// 注册合法 release → 清单可查、sha256/大小与上传内容一致、storage_key 带对象键前缀
func TestAgentReleaseRegisterAndList(t *testing.T) {
	repo := newFakeAgentReleaseRepo()
	uploader := &fakeReleaseUploader{}
	uc := NewAgentReleaseUseCase(repo, uploader)

	body := []byte("node-agent-binary-v0.1.1")
	sum := sha256.Sum256(body)

	rel, err := uc.Register(context.Background(), RegisterParams{
		Version: "v0.1.1", OS: "linux", Arch: "amd64", Body: bytes.NewReader(body),
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if rel.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %s, want %s", rel.SHA256, hex.EncodeToString(sum[:]))
	}
	if rel.SizeBytes != int64(len(body)) {
		t.Errorf("size = %d, want %d", rel.SizeBytes, len(body))
	}
	if rel.Bucket != "cluster" {
		t.Errorf("bucket = %q, want cluster", rel.Bucket)
	}
	if !strings.HasPrefix(rel.StorageKey, "agent-release/") {
		t.Errorf("storage_key = %q, 应为对象键前缀 agent-release/", rel.StorageKey)
	}
	// 上传分块确实把完整内容送出去了
	var got []byte
	for _, c := range uploader.chunks {
		got = append(got, c...)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("uploaded bytes mismatch: %d vs %d", len(got), len(body))
	}

	list, err := uc.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
}

// 非法版本/平台被拒
func TestAgentReleaseInvalidInput(t *testing.T) {
	uc := NewAgentReleaseUseCase(newFakeAgentReleaseRepo(), &fakeReleaseUploader{})
	cases := []RegisterParams{
		{Version: "0.1.1", OS: "linux", Arch: "amd64", Body: strings.NewReader("x")}, // 缺 v 前缀
		{Version: "v0.1.1", OS: "darwin", Arch: "amd64", Body: strings.NewReader("x")}, // 平台不支持
		{Version: "v0.1.1", OS: "linux", Arch: "mips", Body: strings.NewReader("x")},   // 架构不支持
	}
	for i, c := range cases {
		if _, err := uc.Register(context.Background(), c); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

// 重复版本+平台拒绝
func TestAgentReleaseDuplicate(t *testing.T) {
	uc := NewAgentReleaseUseCase(newFakeAgentReleaseRepo(), &fakeReleaseUploader{})
	for i := 0; i < 2; i++ {
		_, err := uc.Register(context.Background(), RegisterParams{
			Version: "v0.1.1", OS: "linux", Arch: "amd64", Body: strings.NewReader("same"),
		})
		if i == 0 && err != nil {
			t.Fatalf("first register: %v", err)
		}
		if i == 1 && err == nil {
			t.Fatal("duplicate register should fail")
		}
	}
}

// 上传通道报错 → Register 返回错误
func TestAgentReleaseUploadError(t *testing.T) {
	uploader := &fakeReleaseUploader{err: fmt.Errorf("asset_service 不可达")}
	uc := NewAgentReleaseUseCase(newFakeAgentReleaseRepo(), uploader)
	_, err := uc.Register(context.Background(), RegisterParams{
		Version: "v0.1.1", OS: "linux", Arch: "amd64", Body: strings.NewReader("body"),
	})
	if err == nil {
		t.Fatal("expected upload error, got nil")
	}
}

// 版本号格式（与 controller proto/前端一致约束）
func TestVersionRe(t *testing.T) {
	good := []string{"v0.1.1", "v1.0.0", "v0.1.1-rc1"}
	bad := []string{"0.1.1", "v0.1", "v0.1.1 ", "V0.1.1", "v0.1.1/..", "abc"}
	for _, g := range good {
		if !versionRe.MatchString(g) {
			t.Errorf("should match: %q", g)
		}
	}
	for _, b := range bad {
		if versionRe.MatchString(b) {
			t.Errorf("should not match: %q", b)
		}
	}
}
