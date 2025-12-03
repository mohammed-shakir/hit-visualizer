# Builder stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Enable CGO for h3-go
ENV CGO_ENABLED=1

RUN apk add --no-cache \
  git=2.49.1-r0 \
  build-base=0.5-r3

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the server binary
RUN go build -o server ./cmd/server

# Final image
FROM alpine:3.20

WORKDIR /app

COPY --from=builder /app/server ./server

COPY frontend ./frontend

RUN apk add --no-cache ca-certificates=20250911-r0

EXPOSE 8081

ENV HTTP_ADDR=:8081

ENTRYPOINT ["./server"]

