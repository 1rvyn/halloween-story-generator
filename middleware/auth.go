package middleware

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/models"
	"github.com/MicahParks/keyfunc"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
)

// Global variable to store JWKS
var jwks *keyfunc.JWKS

// Global variable to store S3 client for R2
var R2Client *s3.Client

// InitializeJWKS initializes the JWKS and should be called during application startup
func InitializeJWKS(auth0Domain string) error {
	jwksURL := "https://" + auth0Domain + "/.well-known/jwks.json"
	options := keyfunc.Options{
		RefreshInterval:   time.Hour,
		RefreshRateLimit:  time.Minute * 5,
		RefreshTimeout:    time.Second * 10,
		RefreshUnknownKID: true,
	}
	var err error
	jwks, err = keyfunc.Get(jwksURL, options)
	if err != nil {
		return fmt.Errorf("failed to get JWKS from Auth0: %w", err)
	}
	return nil
}

// InitializeR2 initializes the Cloudflare R2 client
func InitializeR2() error {
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set")
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("auto"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKey,
			secretKey,
			"",
		)),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	R2Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.EndpointResolver = s3.EndpointResolverFromURL(os.Getenv("R2_DEV_ENDPOINT"))
		o.Region = "auto" // Ensure region is set to "auto" for Cloudflare R2
		o.UsePathStyle = true
	})

	return nil
}

// AuthRequired is a middleware that protects API routes using JWT
func AuthRequired() fiber.Handler {
	return func(c *fiber.Ctx) error {
		fmt.Println("AuthRequired middleware invoked")

		if jwks == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "JWKS not initialized",
			})
		}

		// Get the token from the Authorization header
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing Authorization header",
			})
		}

		tokenString := ""
		fmt.Sscanf(authHeader, "Bearer %s", &tokenString)
		if tokenString == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid Authorization header format",
			})
		}

		// Parse and validate the token
		token, err := jwt.Parse(tokenString, jwks.Keyfunc)
		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid token",
			})
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid token claims",
			})
		}

		fmt.Printf("Token Audience: %v\n", claims["aud"]) // Add this line for debugging

		// Verify audience
		expectedAudience := os.Getenv("AUTH0_AUDIENCE")
		if err := verifyAudience(claims, expectedAudience); err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid audience",
			})
		}

		// Verify issuer
		expectedIssuer := "https://" + os.Getenv("AUTH0_DOMAIN") + "/"
		if err := verifyIssuer(claims, expectedIssuer); err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid issuer",
			})
		}

		// **New Code Starts Here**

		// Extract the 'sub' claim from the token
		sub, ok := claims["sub"].(string)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid token claims: 'sub' missing",
			})
		}

		// Query the database to find the user by Auth0 ID
		var user models.User
		if err := database.DB.Where("auth0_id = ?", sub).First(&user).Error; err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "User not found",
			})
		}

		// Set 'user_id' in locals for route handlers to access
		c.Locals("user_id", user.ID)

		// **New Code Ends Here**

		// Store user information in context
		c.Locals("user", claims)
		return c.Next()
	}
}

func verifyAudience(claims jwt.MapClaims, expectedAudience string) error {
	audValue, ok := claims["aud"]
	if !ok {
		return errors.New("audience claim is missing")
	}

	switch aud := audValue.(type) {
	case string:
		if aud != expectedAudience {
			return errors.New("invalid audience")
		}
	case []interface{}:
		found := false
		for _, a := range aud {
			if aStr, ok := a.(string); ok && aStr == expectedAudience {
				found = true
				break
			}
		}
		if !found {
			return errors.New("invalid audience")
		}
	default:
		return errors.New("invalid audience claim format")
	}

	return nil
}

func verifyIssuer(claims jwt.MapClaims, expectedIssuer string) error {
	iss, ok := claims["iss"].(string)
	if !ok {
		return errors.New("issuer claim is missing or invalid")
	}
	if iss != expectedIssuer {
		return errors.New("invalid issuer")
	}
	return nil
}
