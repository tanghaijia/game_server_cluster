package biz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"controller-go/internal/entity"

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

// 注册合法 release → 清单可查、sha256 与内容一致
func TestAgentReleaseRegisterAndList(t *testing.T) {
	repo := newFakeAgentReleaseRepo()
	store := NewLocalReleaseStore(t.TempDir())
	uc := NewAgentReleaseUseCase(repo, store)

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

	list, err := uc.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}

	// 下载回读内容一致
	got, rc, err := uc.OpenBinary(context.Background(), rel.ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(rc)
	if buf.String() != string(body) {
		t.Errorf("binary mismatch: %q", buf.String())
	}
	if got.SHA256 != rel.SHA256 {
		t.Errorf("release meta mismatch")
	}
}

// 非法版本/平台被拒
func TestAgentReleaseInvalidInput(t *testing.T) {
	uc := NewAgentReleaseUseCase(newFakeAgentReleaseRepo(), NewLocalReleaseStore(t.TempDir()))
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
	uc := NewAgentReleaseUseCase(newFakeAgentReleaseRepo(), NewLocalReleaseStore(t.TempDir()))
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
