BINARY_NAME := go-mosaic
SRC := .

.PHONY: all build test vet clean docker pack run-example help

all: vet test build

build:
	@echo "==> Building $(BINARY_NAME)..."
	go build -v -ldflags="-s -w" -o $(BINARY_NAME) $(SRC)

test:
	@echo "==> Running tests..."
	go test -v -race ./...

vet:
	@echo "==> Running go vet..."
	go vet ./...

tidy:
	@echo "==> Tidying go modules..."
	go mod tidy

clean:
	@echo "==> Cleaning..."
	rm -f $(BINARY_NAME) $(BINARY_NAME).exe database.bin test_output.png
	rm -rf pack pack.zip

docker:
	@echo "==> Building Docker image..."
	docker build -t $(BINARY_NAME):latest .

pack:
	@echo "==> Packaging releases..."
	./pack.sh

run-example: build
	@echo "==> Running example..."
	./$(BINARY_NAME) -src input.png -target test_output.png -lib "./test/the witcher3" -worker 4 -srcsize 32

help:
	@echo "Available targets:"
	@echo "  build        - Build go-mosaic binary"
	@echo "  test         - Run unit and integration tests"
	@echo "  vet          - Run go vet code analysis"
	@echo "  tidy         - Run go mod tidy"
	@echo "  clean        - Remove build artifacts and temporary files"
	@echo "  docker       - Build Docker image"
	@echo "  pack         - Cross-compile for multiple OS/architectures"
	@echo "  run-example  - Build and run an example generation"
