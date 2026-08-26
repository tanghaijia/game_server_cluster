package entity

import "time"

type SteamBranch struct {
	Id          string            `gorm:"column:id;primaryKey"`
	BranchName  string            `gorm:"column:branch_name"`
	LastBuildId uint64            `gorm:"column:last_build_id"`
	Description string            `gorm:"column:description"`
	GameId      string            `gorm:"column:game_id"`
	Status      SteamBranchStatus `gorm:"column:status"`
	// P2-D：保底副本数（§4.3）——0=按需（实例驱动），N=无实例也常驻 N 份（故障域保底）
	MinReplicas int       `gorm:"column:min_replicas"`
	CreateTime  time.Time `gorm:"column:create_time"`
	UpdateTime  time.Time `gorm:"column:update_time"`
}

func (SteamBranch) TableName() string {
	return "steam_branches"
}

type SteamBranchStatus int

const (
	Disable SteamBranchStatus = iota
	Enable
	Abandoned // 废弃
)
