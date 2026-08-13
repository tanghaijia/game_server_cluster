package entity

import "time"

type UserRole int

const (
	RoleUser UserRole = iota
	RoleAdmin
)

type UserStatus int

const (
	UserStatusActive UserStatus = iota
	UserStatusDisabled
)

type User struct {
	ID           string     `gorm:"column:id;primaryKey"`
	Username     string     `gorm:"column:username;uniqueIndex"`
	PasswordHash string     `gorm:"column:password_hash"`
	Role         UserRole   `gorm:"column:role"`
	Status       UserStatus `gorm:"column:status"`
	CreateTime   time.Time  `gorm:"column:create_time"`
	UpdateTime   time.Time  `gorm:"column:update_time"`
}

func (User) TableName() string {
	return "users"
}
