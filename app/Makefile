BINARY := lildocker
BUILD_DIR := ./build
GO := go
GOFLAGS := -v

.PHONY: all build install clean test bootstrap run-example fmt lint

all: build

## build: Compile lildocker binary
build:
	@echo "🔨 Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) .
	@echo "✅ Built → $(BUILD_DIR)/$(BINARY)"

## install: Install lildocker to /usr/local/bin (requires sudo)
install: build
	@echo "📦 Installing $(BINARY) to /usr/local/bin..."
	sudo cp $(BUILD_DIR)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "✅ Installed. Run: lildocker --help"

## bootstrap: Import Alpine Linux base image (requires sudo)
bootstrap:
	@echo "🌱 Bootstrapping Alpine base image..."
	sudo bash bootstrap.sh

## run-example: Build and run the example app (requires bootstrap first)
run-example: build bootstrap
	sudo $(BUILD_DIR)/$(BINARY) build -t myapp:latest ./example
	sudo $(BUILD_DIR)/$(BINARY) run -p 8080:8080 myapp:latest

## clean: Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)
	$(GO) clean

## test: Run unit tests
test:
	$(GO) test ./... -v

## fmt: Format Go source files
fmt:
	$(GO) fmt ./...

## lint: Run go vet
lint:
	$(GO) vet ./...

## images: List images (requires sudo)
images:
	sudo $(BUILD_DIR)/$(BINARY) images

## ps: List containers (requires sudo)
ps:
	sudo $(BUILD_DIR)/$(BINARY) ps

help:
	@echo ""
	@echo "lildocker - Makefile targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
	@echo ""
