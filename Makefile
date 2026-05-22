.PHONY: help gen run run-task build build-task clean install-tools
SOURCE = cmd/server/main.go
BINARY = oss
TASK_BINARY = oss-task
TASK_MODE ?= all

help:
	@echo "Available commands:"
	@echo "  make install-tools - Install required tools (gorm-gen)"
	@echo "  make gen           - Generate GORM models"
	@echo "  make run           - Run the server"
	@echo "  make run-task      - Run timer task process"
	@echo "  make build         - Build the binary"
	@echo "  make build-task    - Build timer task binary"
	@echo "  make clean         - clean the binary"

install-tools:
	go install gorm.io/gen/tools/gentool@latest
	go install github.com/air-verse/air@latest

gen:
	cd adaptor/repo && gentool -c .\gen.yaml

run:
	go run $(SOURCE)

run-task:
	go run ./cmd/task -mode $(TASK_MODE)

build:
	go build -o $(BINARY) $(SOURCE)

build-task:
	go build -o $(TASK_BINARY) ./cmd/task

clean:
	rm -f $(BINARY) $(TASK_BINARY)
