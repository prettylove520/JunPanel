#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
go test ./...
go vet ./...
mkdir -p bin
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -buildvcs=false -trimpath -ldflags='-s -w -buildid=' -o "bin/junpanel-linux-$arch" .
done
(cd bin && sha256sum junpanel-linux-amd64 junpanel-linux-arm64 > SHA256SUMS)
printf '已构建 Linux amd64 / arm64 安装程序。\n'
