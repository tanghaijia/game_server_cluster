package entity

type Game struct {
	ID                string `gorm:"column:id;primaryKey"`
	Name              string `gorm:"column:name"`
	AppId             string `gorm:"column:app_id"`
	ContainerConfigID string `gorm:"column:container_config_id"`
}

func (Game) TableName() string {
	return "games"
}
