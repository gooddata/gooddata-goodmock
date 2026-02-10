# (C) 2025 GoodData Corporation
FROM 020413372491.dkr.ecr.us-east-1.amazonaws.com/pullthrough/docker.io/library/golang:1.25.6-alpine AS builder
WORKDIR /app

# Install CA certificates
RUN apk add --no-cache ca-certificates

# Copy Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the application source code
COPY . .

# Build the Go application
RUN CGO_ENABLED=0 GOOS=linux go build -o goodmock .

FROM scratch
WORKDIR /

# Copy CA certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/goodmock /usr/bin/goodmock

EXPOSE 8080

ENV PORT=8080
ENTRYPOINT ["/usr/bin/goodmock"]
CMD ["replay"]
