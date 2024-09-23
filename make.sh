#!/bin/bash
if [[ "$1" = "t" ]]; then
    go build -race -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)"
elif [[ -n "$1" ]]; then
    CGO_ENABLED=0 go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)"
    GOOS=windows GOARCH=amd64 go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)"
    GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)" -o ytarchive_armv7l
    GOOS=linux GOARCH=arm GOARM=6 go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)" -o ytarchive_armv6
    GOOS=linux GOARCH=mips GOMIPS=softfloat go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)" -o ytarchive_mips
    GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -ldflags "-X main.Commit=-$(git rev-parse --short HEAD)" -o ytarchive_mipsle
else
    CGO_ENABLED=0 go build
    GOOS=windows GOARCH=amd64 go build
    GOOS=linux GOARCH=arm GOARM=7 go build -o ytarchive_armv7l
    GOOS=linux GOARCH=arm GOARM=6 go build -o ytarchive_armv6
    GOOS=linux GOARCH=mips GOMIPS=softfloat go build -o ytarchive_mips
    GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -o ytarchive_mipsle
fi

zip ytarchive_linux_amd64.zip ytarchive
zip ytarchive_windows_amd64.zip ytarchive.exe
zip ytarchive_armv7l.zip ytarchive_armv7l
zip ytarchive_armv6.zip ytarchive_armv6
zip ytarchive_mips.zip ytarchive_mips
zip ytarchive_mipsle.zip ytarchive_mipsle

sha256sum ytarchive_linux_amd64.zip ytarchive_windows_amd64.zip ytarchive_armv7l.zip ytarchive_armv6.zip ytarchive_mips.zip ytarchive_mipsle.zip > SHA2-256SUMS
