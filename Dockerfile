FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /recon ./cmd/recon

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl
COPY --from=builder /recon /recon
COPY migrations/ /migrations/
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD curl -sf http://localhost:8080/health || exit 1
ENTRYPOINT ["/recon", "serve"]
