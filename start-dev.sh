#!/bin/sh
set -e

# Install dependencies
echo "Installing dependencies..."
go mod download
bun install

# Start air for hot-reloading
exec air

