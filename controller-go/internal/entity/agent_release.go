package entity

import "time"

// AgentRelease node_agent 发布版本清单（000031 迁移 + 000033 加 bucket，见
// docs/agent-release-asset-service-redesign.md）。二进制本体由 asset_service 写对象存储，
// 本表存元数据 + 对象定位（bucket/storage_key=object_key）。
type AgentRelease struct {
	ID         string    `gorm:"column:id;primaryKey"`
	Version    string    `gorm:"column:version"`     // 如 v0.1.1
	OS         string    `gorm:"column:os"`          // linux / windows
	Arch       string    `gorm:"column:arch"`        // amd64 / arm64
	SHA256     string    `gorm:"column:sha256"`      // 文件完整性哈希
	SizeBytes  int64     `gorm:"column:size_bytes"`  // 字节
	Bucket     string    `gorm:"column:bucket"`      // 对象所在桶（asset_service 写入时确定）
	StorageKey string    `gorm:"column:storage_key"` // 对象键 object_key（agent-release/{version}/{os}-{arch}/node-agent）
	Note       string    `gorm:"column:note"`
	CreatedBy  string    `gorm:"column:created_by"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (AgentRelease) TableName() string { return "agent_releases" }
