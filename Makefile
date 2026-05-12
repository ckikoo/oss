.PHONY: help gen run build clean install-tools
SOURCE = main.go
BINARY = oss

help:
	@echo "Available commands:"
	@echo "  make install-tools - Install required tools (gorm-gen)"
	@echo "  make gen           - Generate GORM models"
	@echo "  make run           - Run the server"
	@echo "  make build         - Build the binary"
	@echo "  make clean         - clean the binary"

install-tools:
	go install gorm.io/gen/tools/gentool@latest
	go install github.com/air-verse/air@latest

gen:
	cd adaptor/repo && gentool -c .\gen.yaml

run:
	go run $(SOURCE)

build:
	go build -o $(BINARY) $(SOURCE)

clean:
	rm -f $(BINARY)