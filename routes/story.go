package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/halloween-story-generator/database"
	"github.com/yourusername/halloween-story-generator/models"
)

func CreateStory(c *fiber.Ctx) error {
	story := new(models.Story)
	if err := c.BodyParser(story); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	database.DB.Create(&story)

	return c.Status(fiber.StatusCreated).JSON(story)
}

func GetStories(c *fiber.Ctx) error {
	var stories []models.Story
	database.DB.Find(&stories)

	return c.JSON(stories)
}
