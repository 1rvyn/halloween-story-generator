package middleware

import (
	"errors"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
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

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

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
		if err := verifyAudience(claims, auth0Audience); err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid audience",
			})
		}

		// Verify issuer
		if err := verifyIssuer(claims, "https://"+auth0Domain+"/"); err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid issuer",
			})
		}

		// Store user information in context
		c.Locals("user", claims)
		return c.Next()
	}
}

func verifyAudience(claims jwt.MapClaims, expectedAudience string) error {
	aud, ok := claims["aud"].(string)
	if !ok {
		return errors.New("audience claim is missing or invalid")
	}
	if aud != expectedAudience {
		return errors.New("invalid audience")
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
