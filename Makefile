BINARY_NAME = rss2rm
WEB_DIR     = web
WEB_DIST    = $(WEB_DIR)/dist
DATA_DIR    = $(shell pwd)/data
DOCKER_PORT = 8080
DOCKER_ADMIN_PORT = 9090

.PHONY: all build build-web serve serve-ui test test-e2e vet fmt clean docker-build docker-run

# Build everything (frontend + backend).
all: build-web build

# Build the Go binary.
build:
	go build -v -o $(BINARY_NAME) ./cmd/rss2rm

# Build the Web UI.
build-web:
	cd $(WEB_DIR) && npm install && npm run build

# Run the server (API only).
serve: build
	./$(BINARY_NAME) serve -port 8080 -poll

# Run the server with Web UI.
serve-ui: build-web build
	./$(BINARY_NAME) serve -port 8080 -poll -web-dir $(WEB_DIST)

# Run all tests.
test:
	go test ./...

# Run end-to-end tests.
test-e2e: build
	./test/run_all.sh

# Run go vet.
vet:
	go vet ./...

# Format all Go source files.
fmt:
	gofmt -w .

# Remove build artifacts.
clean:
	rm -f $(BINARY_NAME)
	rm -rf $(WEB_DIST)

# Build Docker image.
docker-build:
	docker build -t rss2rm .

# Run Docker container.
# Usage: make docker-run [DATA_DIR=/path/to/data] [DOCKER_PORT=8080] [DOCKER_ADMIN_PORT=9090]
docker-run: docker-build
	@mkdir -p $(DATA_DIR)
	docker run -d -p $(DOCKER_PORT):8080 -p $(DOCKER_ADMIN_PORT):9090 -v $(DATA_DIR):/data rss2rm
