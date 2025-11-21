FROM golang:1.25 AS builder
WORKDIR /app

# Install system dependencies for bun
RUN apt-get update && apt-get install -y unzip

# Install templ and bun for building assets
RUN go install github.com/a-h/templ/cmd/templ@latest
RUN curl -fsSL https://bun.sh/install | bash
ENV PATH="/root/.bun/bin:${PATH}"

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

# Build Go binary
WORKDIR /app/cmd
RUN CGO_ENABLED=0 go build -o /app/app-binary


FROM golang:1.25 AS dev
WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y unzip

# Install Go tools
RUN go install github.com/air-verse/air@v1.52.3
RUN go install github.com/a-h/templ/cmd/templ@latest

# Install bun for JavaScript bundling
RUN curl -fsSL https://bun.sh/install | bash
ENV PATH="/root/.bun/bin:${PATH}"

CMD ["air"]


# Default target for production
FROM debian:bookworm-slim
WORKDIR /app

# Install ca-certificates for HTTPS requests
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/app-binary .
COPY --from=builder /app/static ./static
ENTRYPOINT ["/app/app-binary"]

