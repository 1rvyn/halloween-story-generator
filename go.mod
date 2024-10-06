module github.com/1rvyn/halloween-story-generator

go 1.21

require (
	github.com/MicahParks/keyfunc v1.9.0
	github.com/aws/aws-sdk-go-v2 v1.30.5
	github.com/aws/aws-sdk-go-v2/credentials v1.17.32
	github.com/gofiber/fiber/v2 v2.52.5
	github.com/gofiber/template/html/v2 v2.1.2
	github.com/golang-jwt/jwt/v5 v5.2.1
	gorm.io/gorm v1.25.12
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.4 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.11.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.3.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.11.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.17.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.22.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.26.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.30.7 // indirect
	github.com/aws/smithy-go v1.20.4 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/pgx/v5 v5.5.5 // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
)

require (
	github.com/andybalholm/brotli v1.0.5 // indirect
	github.com/aws/aws-sdk-go v1.55.5
	github.com/aws/aws-sdk-go-v2/config v1.27.33
	github.com/aws/aws-sdk-go-v2/service/s3 v1.61.2
	github.com/gofiber/template v1.8.3 // indirect
	github.com/gofiber/utils v1.1.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.0
	github.com/google/uuid v1.5.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/joho/godotenv v1.5.1
	github.com/klauspost/compress v1.17.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/rivo/uniseg v0.4.4 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.51.0 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	golang.org/x/oauth2 v0.23.0
	golang.org/x/sys v0.15.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	gorm.io/driver/postgres v1.5.9
)

replace github.com/1rvyn/halloween-story-generator/routes => ./routes

replace github.com/1rvyn/halloween-story-generator/middleware => ./middleware

replace github.com/1rvyn/halloween-story-generator/database => ./database

replace github.com/1rvyn/halloween-story-generator/models => ./models

replace github.com/1rvyn/halloween-story-generator/auth => ./auth

// Add any other dependencies your project needs
