#!/bin/bash
export CGO_ENABLED=0
go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)"
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)"

zip ytarchive_linux_amd64.zip ytarchive
zip ytarchive_windows_amd64.zip ytarchive.exe