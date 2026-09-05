package entity

import "time"

// AgentRelease node_agent 发布版本清单（000031 迁移，见 docs/node-agent-upgrade-design.md §3.1）。
// 二进制文件本体由 ReleaseStore 落盘（默认 controller 本地目录），本表只存元数据。
type AgentRelease struct {
	ID         string    `gorm:"column:id;primaryKey"`
	Version    string    `gorm:"column:version"`     // 如 v0.1.1
	OS         string    `gorm:"column:os"`          // linux / windows
	Arch       string    `gorm:"column:arch"`        // amd64 / arm64
	SHA256     string    `gorm:"column:sha256"`      // 文件完整性哈希
	SizeBytes  int64     `gorm:"column:size_bytes"`  // 字节
	StorageKey string    `gorm:"column:storage_key"` // ReleaseStore 内唯一键
	Note       string    `gorm:"column:note"`
	CreatedBy  string    `gorm:"column:created_by"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (AgentRelease) TableName() string { return "agent_releases" }
