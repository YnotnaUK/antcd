# Build Stage
FROM golang:1.27.1-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and compile statically
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /antcd ./cmd/antcd

# Final Stage
FROM gcr.io/distroless/static-debian13:nonroot

LABEL org.opencontainers.image.source="https://github.com/YnotnaUK/antcd" \
      org.opencontainers.image.licenses="MPL-2.0"

WORKDIR /
COPY --from=builder /antcd /antcd

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/antcd"]
