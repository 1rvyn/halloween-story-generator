package routes

// func Dashboard(c *fiber.Ctx) error {
// 	userID, ok := c.Locals("user_id").(uint) // Adjust the type based on your User model
// 	if !ok {
// 		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
// 	}

// 	// Fetch user from database
// 	var user models.User
// 	if err := database.DB.First(&user, userID).Error; err != nil {
// 		log.Printf("Error fetching user: %v", err)
// 		return c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
// 	}

// 	// Log the access
// 	log.Printf("User %s accessed the dashboard", user.Email)

// 	// Render the dashboard template with user info
// 	return c.Render("dashboard", fiber.Map{
// 		"Name":    user.Name,
// 		"Email":   user.Email,
// 		"Picture": user.Picture,
// 	})
// }
