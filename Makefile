BIN   := bin
NAME  := hactl

.PHONY: all test clean $(BIN)/$(NAME)

all: test $(BIN)/$(NAME)

$(BIN)/$(NAME): $(BIN)
	go build -o $(BIN)/$(NAME) .

test:
	go test ./...

clean:
	rm -rf $(BIN)

$(BIN):
	mkdir -p $(BIN)
