package entity

import "time"

// 凭证池状态
const (
	CredentialAvailable = "available" // 可分配
	CredentialInUse     = "in_use"    // 已被某实例占用
	CredentialOrphan    = "orphan"    // 悬挂（占用实例失联等），需管理员 force-release
)

// CredentialPool 外部受限凭证池（M8）：如 DST cluster_token。
// 平台侧全通用——按 game_id + resource_type 池化，管理员从官网创建后录入，
// 实例启动时分配、停止/失败时释放复用。见 adapter-framework-design.md §3.6.5。
type CredentialPool struct {
	ID             string     `gorm:"column:id;primaryKey"`
	GameID         string     `gorm:"column:game_id"`
	ResourceType   string     `gorm:"column:resource_type"`
	Secret         string     `gorm:"column:secret"`
	Status         string     `gorm:"column:status"`
	InstanceID     *string    `gorm:"column:instance_id"`
	LastInstanceID *string    `gorm:"column:last_instance_id"`
	AllocatedAt    *time.Time `gorm:"column:allocated_at"`
	ReleasedAt     *time.Time `gorm:"column:released_at"`
	Remark         string     `gorm:"column:remark"`
	CreateTime     time.Time  `gorm:"column:create_time"`
	UpdateTime     time.Time  `gorm:"column:update_time"`
}

func (CredentialPool) TableName() string {
	return "credential_pool"
}
