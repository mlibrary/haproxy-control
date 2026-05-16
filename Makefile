BIN     := bin
NAME    := hactl
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "DEV")

GOOS   := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

.PHONY: all test clean $(BIN)/$(NAME)-linux-amd64 $(BIN)/$(NAME)

all: test $(BIN)/$(NAME)-linux-amd64 $(BIN)/$(NAME)

$(BIN)/$(NAME)-linux-amd64: $(BIN)
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=$(VERSION)" -o $@ .

$(BIN)/$(NAME): $(BIN)
ifneq ($(GOOS)-$(GOARCH),linux-amd64)
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BIN)/$(NAME)-$(GOOS)-$(GOARCH) .
endif
	ln -sf $(NAME)-$(GOOS)-$(GOARCH) $(BIN)/$(NAME)

test:
	go test ./...

clean:
	rm -rf $(BIN)

$(BIN):
	mkdir -p $(BIN)
