package middleware

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// SessionStore should be initialized in main.go and set via SetSessionStore
var SessionStore *session.Store

// SetSessionStore sets the session store for the middleware
func SetSessionStore(store *session.Store) {
	SessionStore = store
}

// SessionAuthRequired ensures that the user is authenticated via session
func SessionAuthRequired() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if SessionStore == nil {
			log.Println("Session store is not initialized")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Internal Server Error",
			})
		}

		// Retrieve session
		sess, err := SessionStore.Get(c)
		if err != nil {
			log.Printf("Error retrieving session: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Internal Server Error",
			})
		}

		// Check if user_id exists in session
		userID := sess.Get("user_id")
		if userID == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Unauthorized",
			})
		}

		return c.Next()
	}
}
