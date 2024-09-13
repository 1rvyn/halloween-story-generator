package main

import (
	"log"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/middleware"
	"github.com/1rvyn/halloween-story-generator/routes"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
)

func main() {
	// Initialize database
	if err := database.Connect(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Create a new Fiber instance
	app := fiber.New(fiber.Config{
		Views: html.New("./views", ".html"),
	})

	setupRoutes(app)

	log.Println("Server starting on :8080")
	log.Fatal(app.Listen(":8080"))
}

func setupRoutes(app *fiber.App) {
	// Public route
	app.Get("/home", routes.Home)

	// Protected routes
	api := app.Group("/api", middleware.AuthRequired(auth0Domain, auth0Audience))

	api.Post("/story", routes.CreateStory)
	api.Get("/stories", routes.GetStories)
}
