# syntax=docker/dockerfile:1
FROM oven/bun:1 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM golang:1.26.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bot /bot
# owned by the nonroot user so SQLite can write; a new named volume inherits it
COPY --from=build --chown=65532:65532 /out/data /data
ENV DB_PATH=/data/bot.db HTTP_ADDR=:8090
VOLUME /data
EXPOSE 8090
ENTRYPOINT ["/bot"]
