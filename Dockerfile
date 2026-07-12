FROM golang:1.26-alpine AS builder

ARG TARGETARCH
ARG VERSION=dev

WORKDIR /app
COPY src/go.mod src/go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

COPY src/ .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOPROXY=https://goproxy.cn,direct go build -ldflags="-s -w -X main.Version=${VERSION}" -trimpath -o hubproxy .

FROM alpine:3.24.1

WORKDIR /app

COPY --from=builder /app/hubproxy .

CMD ["./hubproxy"]
