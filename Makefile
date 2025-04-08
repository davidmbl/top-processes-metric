# Makefile for top-process-exporter
IMAGE_NAME ?= davidmbl/top-process-exporter
VERSION ?= latest

.PHONY: build build-multi push-multi run clean

# Default target
all: build-multi

# Local build (single platform)
build:
	docker build -t $(IMAGE_NAME):$(VERSION) .

# Multi-platform build and push
build-multi:
	./build.sh $(VERSION)

# Run container locally
run:
	docker-compose up -d

# Stop and remove container
stop:
	docker-compose down

# Clean up
clean:
	docker-compose down
	-docker rmi $(IMAGE_NAME):$(VERSION)

# Help information
help:
	@echo "Available targets:"
	@echo "  build        - Build Docker image for local platform"
	@echo "  build-multi  - Build and push multi-platform Docker image"
	@echo "  run          - Run container with docker-compose"
	@echo "  stop         - Stop running container"
	@echo "  clean        - Remove container and images"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION      - Image version tag (default: latest)"
	@echo "  IMAGE_NAME   - Docker image name (default: davidmbl/top-process-exporter)"
	@echo ""
	@echo "Examples:"
	@echo "  make build-multi VERSION=v1.0.0"
	@echo "  make run"
