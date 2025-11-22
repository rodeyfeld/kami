FROM golang:1.25 AS builder
WORKDIR /app

# Install system dependencies for bun
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN apt-get update && apt-get install -y --no-install-recommends \
    unzip \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install templ
RUN go install github.com/a-h/templ/cmd/templ@v0.3.960

# Install bun from official image
# Copy bun runtime (both bun + bunx helper) for frontend asset builds
COPY --from=oven/bun:1 /usr/local/bin/bun /usr/local/bin/bun
COPY --from=oven/bun:1 /usr/local/bin/bunx /usr/local/bin/bunx

# Copy dependency files
COPY go.mod go.sum package.json ./
RUN go mod download

# Copy source code
COPY . .

# Generate templ files
RUN templ generate

# Build JavaScript and CSS
RUN bun install --frozen-lockfile
RUN bun run build:js
RUN bun run build:css

# Build Go binary (production - no dev tags, uses buildkit's target platform)
WORKDIR /app/cmd
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/app-binary


FROM golang:1.25 AS dev
WORKDIR /app

# Install system dependencies
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN apt-get update && apt-get install -y --no-install-recommends \
    unzip \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install Go tools with pinned versions
RUN go install github.com/air-verse/air@v1.52.3 && \
    go install github.com/a-h/templ/cmd/templ@v0.3.960

# Install bun from official image
COPY --from=oven/bun:1 /usr/local/bin/bun /usr/local/bin/bun

CMD ["air"]


# Default target for production
FROM debian:bookworm-slim
WORKDIR /app

# Install ca-certificates for HTTPS requests
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -u 1000 -m -s /bin/bash kami && \
    mkdir -p /tmp && \
    chown -R kami:kami /app /tmp

USER kami

COPY --from=builder --chown=kami:kami /app/app-binary .
COPY --from=builder --chown=kami:kami /app/static ./static

ENTRYPOINT ["/app/app-binary"]

