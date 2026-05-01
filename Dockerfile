# syntax=docker/dockerfile:1.5
FROM golang:1.22 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o /out/netra-browser ./cmd/netra-browser

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        chromium \
        ca-certificates \
        fonts-liberation \
        libasound2 \
        libnss3 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/netra-browser /usr/local/bin/netra-browser
ENV PATH="/usr/local/bin:${PATH}"
EXPOSE 7878
ENTRYPOINT ["/usr/local/bin/netra-browser"]
CMD ["--listen", "0.0.0.0:7878", "--launch", "--launch-headless"]
