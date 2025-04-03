# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy go module files and source code
COPY go.mod main.go ./

# Get dependencies and build
RUN go mod tidy && \
    go get -d -v && \
    go build -o top-process-exporter .

# Final stage
FROM alpine:latest

# Install necessary dependencies for health checks
RUN apk --no-cache add ca-certificates wget

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/top-process-exporter .

# Metadata
LABEL maintainer="top-services-project"
LABEL version="1.0.0"
LABEL description="Prometheus exporter for top CPU and memory processes"

# Expose the port
EXPOSE 8000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8000/health || exit 1

# Default environment variables
ENV TOP_N=100
ENV CACHE_SECONDS=10
ENV PORT=8000

# Run the application
CMD ["./top-process-exporter"] 