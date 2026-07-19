package entity

type Game struct {
	ID   string `gorm:"column:id;primaryKey"`
	Name string `gorm:"column:name"`
}

func (Game) TableName() string {
	return "games"
}
