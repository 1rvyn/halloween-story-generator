package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"log"
	"strings"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/models"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"golang.org/x/oauth2"
)

type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type Auth0SignupResponse struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

var (
	auth0OauthConfig = &oauth2.Config{
		RedirectURL:  os.Getenv("AUTH0_CALLBACK_URL"),
		ClientID:     os.Getenv("AUTH0_CLIENT_ID"),
		ClientSecret: os.Getenv("AUTH0_CLIENT_SECRET"),
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  fmt.Sprintf("https://%s/authorize", os.Getenv("AUTH0_DOMAIN")),
			TokenURL: fmt.Sprintf("https://%s/oauth/token", os.Getenv("AUTH0_DOMAIN")),
		},
	}
	oauthStateString = "random"
)

func SignupPage(c *fiber.Ctx) error {
	return c.Render("signup", fiber.Map{})
}

func Signup(c *fiber.Ctx) error {
	var req SignupRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}

	// Auth0 Management API endpoint
	url := fmt.Sprintf("https://%s/api/v2/users", os.Getenv("AUTH0_DOMAIN"))

	// Prepare the request body
	body, _ := json.Marshal(map[string]interface{}{
		"email":      req.Email,
		"password":   req.Password,
		"connection": "Username-Password-Authentication",
	})

	// Create the HTTP request
	httpReq, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	httpReq.Header.Add("Content-Type", "application/json")
	httpReq.Header.Add("Authorization", "Bearer "+getManagementAPIToken())

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create user"})
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusCreated {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to create user"})
	}

	var auth0Response Auth0SignupResponse
	json.NewDecoder(resp.Body).Decode(&auth0Response)

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"message": "User created successfully", "user_id": auth0Response.UserID})
}

func getManagementAPIToken() string {
	url := fmt.Sprintf("https://%s/oauth/token", os.Getenv("AUTH0_DOMAIN"))

	payload := strings.NewReader(fmt.Sprintf(`{
		"client_id":"%s",
		"client_secret":"%s",
		"audience":"https://%s/api/v2/",
		"grant_type":"client_credentials"
	}`, os.Getenv("AUTH0_CLIENT_ID"), os.Getenv("AUTH0_CLIENT_SECRET"), os.Getenv("AUTH0_DOMAIN")))

	req, _ := http.NewRequest("POST", url, payload)
	req.Header.Add("content-type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to get management API token: %v", err)
	}
	defer res.Body.Close()

	var response struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(res.Body).Decode(&response)

	return response.AccessToken
}

func LoginWithGoogle(c *fiber.Ctx) error {
	url := auth0OauthConfig.AuthCodeURL(oauthStateString,
		oauth2.SetAuthURLParam("connection", "google-oauth2"),
		oauth2.SetAuthURLParam("audience", os.Getenv("AUTH0_AUDIENCE")), // Add audience parameter
	)
	return c.Redirect(url)
}

func Callback(c *fiber.Ctx) error {
	state := c.Query("state")
	if state != oauthStateString {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid oauth state")
	}

	code := c.Query("code")
	token, err := auth0OauthConfig.Exchange(context.Background(), code)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Code exchange failed")
	}

	client := auth0OauthConfig.Client(context.Background(), token)
	resp, err := client.Get(fmt.Sprintf("https://%s/userinfo", os.Getenv("AUTH0_DOMAIN")))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Failed getting user info")
	}
	defer resp.Body.Close()

	var userInfo map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Failed decoding user info")
	}

	// Check if user exists in database, if not create new user
	var user models.User
	result := database.DB.Where("auth0_id = ?", userInfo["sub"]).First(&user)
	if result.Error != nil {
		// User doesn't exist, create new user
		user = models.User{
			Email:         userInfo["email"].(string),
			Name:          userInfo["name"].(string),
			Picture:       userInfo["picture"].(string),
			Auth0ID:       userInfo["sub"].(string),
			EmailVerified: userInfo["email_verified"].(bool),
		}
		if err := database.DB.Create(&user).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Failed to create user in database")
		}
	} else {
		// User exists, update information
		user.Email = userInfo["email"].(string)
		user.Name = userInfo["name"].(string)
		user.Picture = userInfo["picture"].(string)
		user.EmailVerified = userInfo["email_verified"].(bool)
		if err := database.DB.Save(&user).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Failed to update user in database")
		}
	}

	// Set a cookie with the JWT token
	c.Cookie(&fiber.Cookie{
		Name:     "jwt",
		Value:    token.AccessToken,
		Expires:  time.Now().Add(time.Hour * 24),
		HTTPOnly: false, // Allow JavaScript to access the cookie
	})

	// Store user ID in session instead of full user info
	sess, err := store.Get(c)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error getting session")
	}

	sess.Set("user_id", user.ID)
	if err := sess.Save(); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error saving session")
	}

	// Redirect to the correct dashboard route
	return c.Redirect("/dashboard") // Now correctly protected by session
}

// ViewStory handles the GET /story route
func ViewStory(c *fiber.Ctx) error {
	// Implement logic to display the story
	return c.Render("story", fiber.Map{
		"Title": "Write Your Story",
	})
}

var store *session.Store

func SetStore(s *session.Store) {
	store = s
}
