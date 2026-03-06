.PHONY: build run clean test

# Build the bot
build:
	go build -o bin/bot main.go

# Run the bot locally
run:
	go run main.go

# Run with token (for testing)
run-local:
	TELEGRAM_BOT_TOKEN=$(TELEGRAM_BOT_TOKEN) go run main.go

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f processed_releases.json

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Download dependencies
deps:
	go mod tidy
	go mod download

# Build for production
build-prod:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/bot-linux-amd64 main.go
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/bot-windows-amd64.exe main.go
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o bin/bot-darwin-amd64 main.go

# Docker build
docker-build:
	docker build -t github-release-bot .

# Docker run
docker-run:
	docker run --rm -e TELEGRAM_BOT_TOKEN=$(TELEGRAM_BOT_TOKEN) github-release-bot
