FROM --platform=$BUILDPLATFORM python:3.12-alpine as builder

# Install build dependencies
RUN apk add --no-cache --virtual .build-deps \
    gcc \
    musl-dev \
    python3-dev \
    linux-headers

# Create virtual environment
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

FROM --platform=$TARGETPLATFORM python:3.12-alpine

# Copy virtual environment from builder
COPY --from=builder /opt/venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Create non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app

# Copy application
COPY exporter.py /app/
RUN chmod +x /app/exporter.py

# Metadata
LABEL maintainer="top-services-project"
LABEL version="1.0.0"
LABEL description="Prometheus exporter for top CPU and memory processes"

# Configure container
USER appuser
EXPOSE 8000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8000/health || exit 1

# Default environment variables
ENV TOP_N=100
ENV CACHE_SECONDS=10
ENV PORT=8000

# Run the exporter
CMD ["python", "/app/exporter.py"] 