package middleware

import (
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/gofiber/fiber"
	"github.com/golang-jwt/jwt"
)

func AuthRequired(auth0Domain, auth0Audience string) fiber.Handler {
	// Fetch JWKS from Auth0
	jwksURL := "https://" + auth0Domain + "/.well-known/jwks.json"
	options := keyfunc.Options{
		RefreshInterval:   time.Hour,
		RefreshRateLimit:  time.Minute * 5,
		RefreshTimeout:    time.Second * 10,
		RefreshUnknownKID: true,
	}
	jwks, err := keyfunc.Get(jwksURL, options)
	if err != nil {
		panic("Failed to get JWKS from Auth0: " + err.Error())
	}

	return func(c *fiber.Ctx) error {
		// Get the token from the Authorization header
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing Authorization header",
			})
		}

		tokenString := authHeader[len("Bearer "):]

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

		// Verify audience
		if !claims.VerifyAudience(auth0Audience, true) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid audience",
			})
		}

		// Verify issuer
		if !claims.VerifyIssuer("https://"+auth0Domain+"/", true) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid issuer",
			})
		}

		// Store user information in context
		c.Locals("user", claims)
		return c.Next()
	}
}
