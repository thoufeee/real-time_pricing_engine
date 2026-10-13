
# Stage 1: Build the Go application
FROM golang:alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /pricing-engine ./cmd/server

# Stage 2: Minimal runtime image
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /pricing-engine ./pricing-engine

EXPOSE 8080

ENTRYPOINT ["./pricing-engine"]
