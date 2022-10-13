#!/bin/bash
export CGO_ENABLED=0

if [[ -n "$1" ]]; then
    go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)"
    GOOS=windows GOARCH=amd64 go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)"
else
    go build
    GOOS=windows GOARCH=amd64 go build
fi

zip ytarchive_linux_amd64.zip ytarchive
zip ytarchive_windows_amd64.zip ytarchive.exe

sha256sum ytarchive_linux_amd64.zip ytarchive_windows_amd64.zip > SHA2-256SUMS