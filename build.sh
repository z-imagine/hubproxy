#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${SCRIPT_DIR}"

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo 'dev')}"
mkdir -p build

echo "=== 编译 linux/amd64 ==="
cd src
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -trimpath -o ../build/hubproxy-linux-amd64 .
cd "${SCRIPT_DIR}"

echo "=== 编译 linux/arm64 ==="
cd src
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -trimpath -o ../build/hubproxy-linux-arm64 .
cd "${SCRIPT_DIR}"

echo "=== 打包 ==="
for arch in amd64 arm64; do
    PKG="build/hubproxy-${VERSION}-linux-${arch}"
    rm -rf "${PKG}" "${PKG}.tar.gz"
    mkdir -p "${PKG}"
    cp build/hubproxy-linux-${arch} "${PKG}/hubproxy"
    cp src/config.toml "${PKG}/config.toml"
    chmod +x "${PKG}/hubproxy"
    tar czf "${PKG}.tar.gz" -C build "hubproxy-${VERSION}-linux-${arch}"
    rm -rf "${PKG}"
    echo "  ${PKG}.tar.gz"
done

echo ""
echo "部署: scp build/hubproxy-${VERSION}-linux-amd64.tar.gz 远程机器:/tmp/"
echo "远程: tar xzf hubproxy-*.tar.gz && cd hubproxy-* && ./hubproxy"
