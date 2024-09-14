package routes

import (
	"fmt"
	"log"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/models"
	"github.com/gofiber/fiber/v2"
)

func Dashboard(c *fiber.Ctx) error {
	// Retrieve session
	sess, err := store.Get(c)
	if err != nil {
		log.Printf("Error retrieving session: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
	}

	fmt.Println(sess.Get("user_id"))

	// Get user ID from session
	userID, ok := sess.Get("user_id").(uint)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	// Fetch user from database
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		log.Printf("Error fetching user: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
	}

	// Log the access
	log.Printf("User %s accessed the dashboard", user.Email)

	// Render the dashboard template with user info
	return c.Render("dashboard", fiber.Map{
		"Name":    user.Name,
		"Email":   user.Email,
		"Picture": user.Picture,
	})
}
