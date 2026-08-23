FROM golang:1.26.6-alpine3.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o server .

FROM alpine:3.24.1
RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Shanghai

WORKDIR /app
RUN addgroup -S -g 10001 cloud && adduser -S -D -H -u 10001 -G cloud cloud \
    && mkdir -p /app/data /app/backups /app/uploads \
    && chown -R cloud:cloud /app
COPY --from=builder --chown=cloud:cloud /app/server .
COPY --from=builder --chown=cloud:cloud /app/config ./config

USER cloud

EXPOSE 8080 1883

CMD ["./server"]
