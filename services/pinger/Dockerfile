# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /go/src/pinger

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o app ./cmd/pinger

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /go/src/pinger/app /usr/local/bin/pinger

EXPOSE 8000

ENTRYPOINT ["/app/pinger"]
