# Start from a base image with Go installed
FROM golang:1.22-alpine as builder

# Set environment variables
ENV GO111MODULE=on

# Create an app directory
WORKDIR /app

# Copy go.mod and go.sum to download dependencies
COPY go.mod go.sum ./
RUN go mod download
RUN go mod tidy

# Copy the source code
COPY . .

# Build the application
RUN go build -o docker-metrics-exporter

# Final image
FROM alpine:3.18

# Create a directory for the app
WORKDIR /app

# Copy the compiled Go binary and any additional files from the builder stage
COPY --from=builder /app/docker-metrics-exporter /app/docker-metrics-exporter

# Install Docker client to allow connecting to Docker daemon
RUN apk add --no-cache docker-cli

# Set the container's entrypoint to the binary, allowing args to be passed at runtime
ENTRYPOINT ["/app/docker-metrics-exporter"]

# Expose default port for Prometheus metrics
EXPOSE 8000