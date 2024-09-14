package database

import (
	"log"
	"path/filepath"
	"time"

	"github.com/1rvyn/halloween-story-generator/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Connect() error {
	dbPath := filepath.Join("/litefs", "halloween_stories.db")
	log.Printf("Attempting to connect to database at: %s", dbPath)

	// Retry mechanism
	var err error
	for i := 0; i < 30; i++ { // Try for 30 seconds
		DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})
		if err == nil {
			log.Printf("Successfully connected to database")
			break
		}
		log.Printf("Failed to connect to database, retrying in 1 second. Error: %v", err)
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		return err
	}

	// Drop and recreate the users table to add PRIMARY KEY
	if DB.Migrator().HasTable(&models.User{}) {
		if err := DB.Migrator().DropTable(&models.User{}); err != nil {
			return err
		}
	}
	if err := DB.AutoMigrate(&models.User{}); err != nil {
		return err
	}

	// Drop and recreate the stories table to ensure consistency
	if DB.Migrator().HasTable(&models.Story{}) {
		if err := DB.Migrator().DropTable(&models.Story{}); err != nil {
			return err
		}
	}
	if err := DB.AutoMigrate(&models.Story{}); err != nil {
		return err
	}

	return nil
}
