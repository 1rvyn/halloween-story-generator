# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Run stage
FROM alpine:latest

WORKDIR /app

# Install ffmpeg (includes ffprobe)
RUN apk add --no-cache ffmpeg

COPY --from=builder /app/main .

# Expose the port your app listens on
EXPOSE 8080

# Remove the following ENV lines to rely on runtime environment variables
# ENV AUTH0_DOMAIN=${AUTH0_DOMAIN}
# ENV AUTH0_AUDIENCE=${AUTH0_AUDIENCE}
# ENV AUTH0_CLIENT_ID=${AUTH0_CLIENT_ID}
# ENV AUTH0_CLIENT_SECRET=${AUTH0_CLIENT_SECRET}
# ENV AUTH0_CALLBACK_URL=${AUTH0_CALLBACK_URL}

CMD ["./main"]

# Ensure the views directory is included in the Docker image
COPY ./views ./views
