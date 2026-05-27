FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o cctv-bot .

FROM alpine:3.21

RUN apk add --no-cache ffmpeg && adduser -D -u 1000 cctv \
 && mkdir -p /data && chown cctv:cctv /data

COPY --from=builder /app/cctv-bot /usr/local/bin/cctv-bot

USER cctv
ENV CAMERAS_FILE=/data/cameras.json
VOLUME ["/data"]
ENTRYPOINT ["cctv-bot"]
