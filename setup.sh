#!/usr/bin/env sh
# Run this once after cloning to generate go.sum before building.
# Requires Go 1.22+ to be installed locally.
set -e
echo "→ Running go mod tidy to generate go.sum..."
go mod tidy
echo "✅ go.sum generated. You can now run:"
echo "   docker build -t telemod ."
echo "   # or"
echo "   go run ./cmd/bot"
