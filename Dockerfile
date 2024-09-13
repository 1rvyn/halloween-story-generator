FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o main .

FROM alpine:latest

RUN apk add --no-cache ca-certificates fuse3 sqlite

COPY --from=flyio/litefs:0.5 /usr/local/bin/litefs /usr/local/bin/litefs
COPY --from=builder /app/main /app/main

COPY litefs.yml /etc/litefs.yml
COPY litefs.local.yml /etc/litefs.local.yml

CMD ["sh", "-c", "if [ \"$FLY_REGION\" = \"local\" ]; then litefs mount -config /etc/litefs.local.yml; else litefs mount; fi"]