FROM golang:1.26 AS builder

WORKDIR /build

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Use a minimal base image for final deployment
FROM alpine:latest

# Set working directory in the container
WORKDIR /app/

# Install ffmpeg to image
RUN apk --no-cache add ffmpeg

# Copy the built binary from the builder stage
COPY --from=builder /build/main .

COPY /drone/prototypes.example.json ./drone/prototypes.json

# Expose the application port
EXPOSE 5001

# Run the application
CMD ["./main", "serve", "-proto", "http", "-p", "5001"]