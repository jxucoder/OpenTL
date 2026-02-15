# TeleCoder Makefile

.PHONY: build install clean test lint sandbox-image server-image docker-up docker-down

# Build the telecoder binary.
build:
	go build -o bin/telecoder ./cmd/telecoder

# Install the telecoder binary to $GOPATH/bin.
install:
	go install ./cmd/telecoder

# Run tests.
test:
	go test ./...

# Run linter.
lint:
	golangci-lint run ./...

# Build the sandbox Docker image.
sandbox-image:
	docker build -f docker/base.Dockerfile -t telecoder-sandbox .

# Build the server Docker image.
server-image:
	docker build -f docker/server.Dockerfile -t telecoder-server .

# Start everything with Docker Compose.
docker-up: sandbox-image
	docker compose -f docker/compose.yml --env-file .env up -d

# Stop everything.
docker-down:
	docker compose -f docker/compose.yml --env-file .env down

# Clean build artifacts.
clean:
	rm -rf bin/
	go clean
