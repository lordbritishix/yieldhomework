# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev pkgconfig librdkafka-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates librdkafka-dev

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Create logs directory
RUN mkdir -p /app/logs

# Expose any ports if needed (none for this crawler)
# EXPOSE 8080

# Run the binary
CMD ["./main"]