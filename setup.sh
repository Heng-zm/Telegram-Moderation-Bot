#!/usr/bin/env sh
# Run this once after cloning to verify modules and tests.
# Requires Go 1.23+.
set -e

echo "→ Tidying Go modules..."
go mod tidy

echo "→ Running tests..."
go test ./...

echo "✅ Ready. Useful commands:"
echo "   go run ./cmd/telemod"
echo "   docker build -t telemod ."
