# ---- build ----
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server ./cmd/server

# ---- run ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 app
COPY --from=build /out/server /server
USER app
EXPOSE 8080
# PORT is read by the server (see .env.example); default matches EXPOSE.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT:-8080}/healthz" >/dev/null || exit 1
ENTRYPOINT ["/server"]
