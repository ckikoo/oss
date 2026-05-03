.PHONY: help gen run build install-tools

help:
	@echo "Available commands:"
	@echo "  make install-tools - Install required tools (gorm-gen)"
	@echo "  make gen           - Generate GORM models"
	@echo "  make run           - Run the server"
	@echo "  make build         - Build the binary"

install-tools:
	go install github.com/go-echarts/go-echarts/v2@latest
	go install gorm.io/gen/tools/gentool@latest

gen:
	cd adaptor/repo && gentool -c .\gen.yaml

run:
	go run ./cmd/server/main.go

build:
	go build -o oss ./cmd/server/main.go
