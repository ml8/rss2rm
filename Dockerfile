# Stage 1: Build Web Frontend
FROM node:20-alpine AS web-builder
WORKDIR /app/web
COPY web/package.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# Stage 2: Build Go Binary
FROM golang:1.25.6-trixie AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags="-w -s" -o rss2rm ./cmd/rss2rm

# Stage 3: Final Image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    python3 \
    python3-pip \
    python3-venv \
    libpango-1.0-0 \
    libpangoft2-1.0-0 \
    libjpeg62-turbo \
    libopenjp2-7 \
    libffi-dev \
    && rm -rf /var/lib/apt/lists/*

RUN python3 -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
RUN pip install weasyprint==63.1

WORKDIR /app

COPY --from=go-builder /app/rss2rm .
COPY --from=web-builder /app/web/dist ./web-dist

RUN mkdir -p /data /tmp/rss2rm && \
    useradd -r -m -s /bin/false rss2rm && \
    chown -R rss2rm:rss2rm /data /tmp/rss2rm /app

USER rss2rm

EXPOSE 8080 9090

CMD ["./rss2rm", "serve", "-port", "8080", "-poll", "-destinations", "remarkable", "-db-dsn", "/data/rss2rm.db", "-web-dir", "./web-dist"]
