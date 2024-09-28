package database

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/1rvyn/halloween-story-generator/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Connect() error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is not set")
	}
	log.Printf("Attempting to connect to database at: %s", dbURL)

	// Retry mechanism
	var err error
	for i := 0; i < 30; i++ { // Try for 30 seconds
		DB, err = gorm.Open(postgres.Open(dbURL), &gorm.Config{
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

	// Automatically migrate your schema
	if err := DB.AutoMigrate(&models.User{}, &models.Story{}, &models.Segment{}); err != nil {
		return err
	}

	return nil
}
