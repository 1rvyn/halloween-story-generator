module github.com/1rvyn/halloween-story-generator

go 1.21

require (
	github.com/MicahParks/keyfunc v1.9.0
	github.com/gofiber/fiber/v2 v2.52.5
	github.com/gofiber/template/html/v2 v2.1.2
	github.com/golang-jwt/jwt/v5 v5.2.1
	gorm.io/driver/sqlite v1.5.6
	gorm.io/gorm v1.25.12
)

require cloud.google.com/go/compute/metadata v0.3.0 // indirect

require (
	github.com/andybalholm/brotli v1.0.5 // indirect
	github.com/gofiber/template v1.8.3 // indirect
	github.com/gofiber/utils v1.1.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.4.2 // indirect
	github.com/google/uuid v1.5.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/joho/godotenv v1.5.1
	github.com/klauspost/compress v1.17.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/mattn/go-sqlite3 v1.14.22 // indirect
	github.com/rivo/uniseg v0.4.4 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.51.0 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	golang.org/x/oauth2 v0.23.0
	golang.org/x/sys v0.15.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace github.com/1rvyn/halloween-story-generator/routes => ./routes

replace github.com/1rvyn/halloween-story-generator/middleware => ./middleware

replace github.com/1rvyn/halloween-story-generator/database => ./database

replace github.com/1rvyn/halloween-story-generator/models => ./models

replace github.com/1rvyn/halloween-story-generator/auth => ./auth

// Add any other dependencies your project needs
