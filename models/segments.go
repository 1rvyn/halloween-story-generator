package models

import "gorm.io/gorm"

type Segment struct {
	gorm.Model
	ContentID int     `json:"content_id"`
	StoryID   int     `json:"story_id"`
	Segment   string  `json:"segment"`
	Number    int     `json:"number"`    // If using integer for segment number
	ImageURL  string  `json:"image_url"` // New field to store image URL
	Duration  float64 `json:"duration"`  // New field to store duration
	ImageData []byte  `json:"-"`         // exclude from gorm auto-migrate
}
