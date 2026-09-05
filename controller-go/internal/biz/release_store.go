package biz

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReleaseStore node_agent 发布二进制的存储抽象（docs/node-agent-upgrade-design.md §3.1）。
// 默认实现 LocalReleaseStore（controller 本地目录，便于部署与冒烟）；
// 如需对象存储可新增实现（同接口，S3/MinIO），controller 无强 S3 依赖。
type ReleaseStore interface {
	// Put 保存二进制并返回 storage_key、sha256、字节数（调用方负责关闭 reader）
	Put(key string, r io.Reader) (string, string, int64, error)
	// Open 打开二进制（storage_key 定位）
	Open(key string) (io.ReadCloser, error)
	// Delete 删除二进制（release 删除时回收）
	Delete(key string) error
}

// LocalReleaseStore 本地目录实现：文件名为 storage_key（安全化后），无路径穿越。
type LocalReleaseStore struct {
	dir string
}

func NewLocalReleaseStore(dir string) *LocalReleaseStore {
	return &LocalReleaseStore{dir: dir}
}

// safeKey 只允许 [A-Za-z0-9._-]，杜绝路径穿越
func safeKey(key string) (string, error) {
	for _, c := range key {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '-' || c == '_'
		if !ok {
			return "", fmt.Errorf("invalid storage key: %q", key)
		}
	}
	if key == "" || key == "." || key == ".." {
		return "", fmt.Errorf("invalid storage key: %q", key)
	}
	return key, nil
}

func (s *LocalReleaseStore) Put(key string, r io.Reader) (string, string, int64, error) {
	k, err := safeKey(key)
	if err != nil {
		return "", "", 0, err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", "", 0, fmt.Errorf("mkdir release dir: %w", err)
	}
	// staging 先写临时文件，成功后原子 rename（防半截文件被下载）
	tmp := filepath.Join(s.dir, k+".tmp")
	dst := filepath.Join(s.dir, k)
	f, err := os.Create(tmp)
	if err != nil {
		return "", "", 0, fmt.Errorf("create staging: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return "", "", 0, fmt.Errorf("write release: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", "", 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", "", 0, fmt.Errorf("rename release: %w", err)
	}
	return k, hex.EncodeToString(h.Sum(nil)), n, nil
}

func (s *LocalReleaseStore) Open(key string) (io.ReadCloser, error) {
	k, err := safeKey(key)
	if err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(s.dir, k))
}

func (s *LocalReleaseStore) Delete(key string) error {
	k, err := safeKey(key)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(s.dir, k))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
