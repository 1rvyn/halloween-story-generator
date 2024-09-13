package main

import (
	"fmt"
	"log"
	"os"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/middleware"
	"github.com/1rvyn/halloween-story-generator/routes"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"github.com/joho/godotenv"
)

var (
	auth0Domain       string
	auth0Audience     string
	auth0ClientID     string
	auth0ClientSecret string
	auth0CallbackURL  string
)

func init() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	auth0Domain = os.Getenv("AUTH0_DOMAIN")
	auth0Audience = os.Getenv("AUTH0_AUDIENCE")
	auth0ClientID = os.Getenv("AUTH0_CLIENT_ID")
	auth0ClientSecret = os.Getenv("AUTH0_CLIENT_SECRET")
	auth0CallbackURL = os.Getenv("AUTH0_CALLBACK_URL")

	if auth0Domain == "" || auth0Audience == "" || auth0ClientID == "" || auth0ClientSecret == "" || auth0CallbackURL == "" {
		log.Fatal("All Auth0 environment variables must be set")
	}

	fmt.Printf("Auth0 Domain: %s, Auth0 Audience: %s, Callback URL: %s\n",
		auth0Domain, auth0Audience, auth0CallbackURL)
}

func main() {
	// Initialize database

	fmt.Printf("Auth0 Domain: %s, Auth0 Audience: %s\n", auth0Domain, auth0Audience)

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
	// Public routes
	app.Get("/home", routes.Home)
	app.Get("/signup", routes.SignupPage)
	app.Post("/signup", routes.Signup)

	// Protected routes
	api := app.Group("/api", middleware.AuthRequired(auth0Domain, auth0Audience))

	api.Post("/story", routes.CreateStory)
	api.Get("/stories", routes.GetStories)

	// Google login routes
	app.Get("/login/google", routes.LoginWithGoogle)
	app.Get("/callback", routes.Callback)
}
