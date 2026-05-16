BIN     := bin
NAME    := hactl
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "DEV")

.PHONY: all test clean $(BIN)/$(NAME)

all: test $(BIN)/$(NAME)

$(BIN)/$(NAME): $(BIN)
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BIN)/$(NAME) .

test:
	go test ./...

clean:
	rm -rf $(BIN)

$(BIN):
	mkdir -p $(BIN)
