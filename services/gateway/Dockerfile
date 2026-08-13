# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY . .
RUN go mod download

RUN go build -o gateway ./cmd/gateway

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

RUN addgroup -g 1000 appgroup && adduser -u 1000 -G appgroup -D appuser

WORKDIR /app

COPY --from=builder /app/gateway .

USER appuser

EXPOSE 8000

ENTRYPOINT ["./gateway"]
