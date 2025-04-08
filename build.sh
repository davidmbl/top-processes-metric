#!/bin/bash

# build.sh - Multi-platform Docker build script
# Usage: ./build.sh [version tag]
#
# If no version tag is provided, "latest" will be used
# Example: ./build.sh v1.0.0

# Configuration
IMAGE_NAME="davidmbl/top-process-exporter"
PLATFORMS="linux/amd64,linux/arm64"
TAG=${1:-latest}

# Ensure builder exists and is ready
if ! docker buildx inspect multiplatform-builder &>/dev/null; then
    echo "Creating new buildx builder: multiplatform-builder"
    docker buildx create --name multiplatform-builder --use
else
    echo "Using existing buildx builder: multiplatform-builder"
    docker buildx use multiplatform-builder
fi

# Ensure builder is running
docker buildx inspect --bootstrap

# Build and push for multiple platforms
echo "Building and pushing multi-platform image: $IMAGE_NAME:$TAG"
docker buildx build \
    --platform $PLATFORMS \
    -t "$IMAGE_NAME:$TAG" \
    --push \
    .

# If this is a version tag (not 'latest'), also tag as latest
if [ "$TAG" != "latest" ]; then
    echo "Also tagging as latest"
    docker buildx build \
        --platform $PLATFORMS \
        -t "$IMAGE_NAME:latest" \
        --push \
        .
fi

echo "Multi-platform build complete for $PLATFORMS"
echo "Image: $IMAGE_NAME:$TAG is now available"
