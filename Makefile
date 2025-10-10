.PHONY: build docker-push-dev clean test

DEV_USER ?= nvanthao
# Build the Go binary locally
build:
	@echo "Building Go binary..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/velero-plugin-cnpg-restore .

# Push to ttl.sh for quick testing (24h expiry)
docker-push-dev:
	$(eval IMAGE_UUID := $(shell uuidgen | tr '[:upper:]' '[:lower:]'))
	@echo "Building and pushing to ttl.sh (24h expiry)..."
	@echo "Image tag: $(IMAGE_UUID)"
	docker build --platform linux/amd64 -t ttl.sh/$(DEV_USER)/velero-plugin-cnpg-restore:$(IMAGE_UUID) .
	docker push ttl.sh/$(DEV_USER)/velero-plugin-cnpg-restore:$(IMAGE_UUID)
	@echo ""
	@echo "Image available at: ttl.sh/$(DEV_USER)/velero-plugin-cnpg-restore:$(IMAGE_UUID)"
	@echo ""
	@echo "To install plugin, run:"
	@echo "  velero plugin add ttl.sh/$(DEV_USER)/velero-plugin-cnpg-restore:$(IMAGE_UUID)"

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	go clean

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Run locally (for development/testing)
run: build
	@echo "Running plugin locally..."
	./bin/velero-plugin-cnpg-restore
