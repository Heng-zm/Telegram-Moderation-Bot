# ── Build stage ──────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /src

# Cache dependency downloads first. go.mod already includes required indirect deps.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -mod=readonly -trimpath -ldflags="-s -w" -o /telemod ./cmd/telemod

# ── Runtime stage ─────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /telemod /app/telemod

EXPOSE 8080
ENTRYPOINT ["/app/telemod"]
