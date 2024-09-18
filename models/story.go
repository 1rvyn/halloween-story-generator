package models

import (
	"time"

	"gorm.io/gorm"
)

type Story struct {
	gorm.Model
	Content   string    `json:"content" gorm:"text"`
	CreatedBy int       `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Response  string    `json:"response" gorm:"text"` // New field to store API response
	VideoURL  string    `json:"url"`
}
