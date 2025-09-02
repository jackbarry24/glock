.PHONY: help build-server build-client build-all test-server test-client test-all clean swagger-generate client-generate docker-build docker-run

# Default target
help:
	@echo "Available targets:"
	@echo "  build-server     - Build the glock-server"
	@echo "  build-client     - Build the glock client"
	@echo "  build-all        - Build both server and client"
	@echo "  test-server      - Run server tests"
	@echo "  test-client      - Run client tests"
	@echo "  test-all         - Run all tests"
	@echo "  clean            - Clean build artifacts"
	@echo "  swagger-generate - Generate OpenAPI/Swagger documentation"
	@echo "  client-generate  - Generate client from OpenAPI spec"
	@echo "  docker-build     - Build Docker image"
	@echo "  docker-run       - Run Docker container"

# Build targets
build-server:
	@echo "Building glock-server..."
	cd glock-server && go build -o ../bin/glock-server ./cmd

build-client:
	@echo "Building glock client..."
	cd glock && go build -o ../bin/glock-client ./

build-all: build-server build-client

# Test targets
test-server:
	@echo "Running server tests..."
	cd glock-server && go test -v ./...

test-client:
	@echo "Running client tests..."
	cd glock && go test -v ./...

test-all: test-server test-client

# Clean target
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -rf glock-server/docs/
	rm -f glock-server/main.go

# Swagger/OpenAPI generation
swagger-generate:
	@echo "Generating OpenAPI/Swagger documentation..."
	cd glock-server && swag init -g cmd/main.go -o docs/
	@echo "Swagger documentation generated in glock-server/docs/"

# Client generation from OpenAPI spec
client-generate: swagger-generate
	@echo "Generating client from OpenAPI spec..."
	@if command -v openapi-generator-cli >/dev/null 2>&1 && command -v java >/dev/null 2>&1; then \
		openapi-generator-cli generate \
			-i glock-server/docs/swagger.json \
			-g go \
			-o glock-generated \
			--additional-properties=packageName=glock,withGoMod=false,enumClassPrefix=true; \
		echo "Client generated in glock-generated/ directory"; \
		echo "To sync with glock/ directory, you may need to manually copy relevant files"; \
	else \
		echo "Prerequisites not met for client generation:"; \
		echo "1. Install Java: https://adoptium.net/"; \
		echo "2. Install OpenAPI Generator CLI: npm install -g @openapitools/openapi-generator-cli"; \
		exit 1; \
	fi

# Docker targets
docker-build:
	@echo "Building Docker image..."
	docker build -t glock-server:latest .

docker-run:
	@echo "Running Docker container..."
	docker run -p 8080:8080 glock-server:latest

# Development targets
dev-server:
	@echo "Starting development server..."
	cd glock-server && go run cmd/main.go

dev-client-test:
	@echo "Running client integration tests..."
	cd glock && go test -v -tags=integration ./...

# CI/CD helper targets
ci-build: build-all test-all swagger-generate

ci-release: ci-build client-generate
