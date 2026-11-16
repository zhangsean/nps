#!/bin/bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -extldflags -static -extldflags -static" ./cmd/nps/nps.go
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -extldflags -static -extldflags -static" ./cmd/npc/npc.go
ls -lh nps.exe npc.exe