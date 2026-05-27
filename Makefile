NAME    := hactl
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "DEV")

GOOS   := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

.PHONY: all test clean install release deb

all: test bin/$(NAME)-linux-amd64 bin/$(NAME)

bin/$(NAME)-linux-amd64: bin
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=$(VERSION)" -o $@ .

bin/$(NAME): bin
ifneq ($(GOOS)-$(GOARCH),linux-amd64)
	go build -ldflags "-X main.Version=$(VERSION)" -o bin/$(NAME)-$(GOOS)-$(GOARCH) .
endif
	ln -sf $(NAME)-$(GOOS)-$(GOARCH) bin/$(NAME)

test:
	go test ./...

clean:
	rm -rf bin

bin:
	mkdir -p bin

release:
	mkdir -p bin/release
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$(VERSION)" -o bin/release/$(NAME) .

install: release
	install -D -m 0755 bin/release/$(NAME) $(DESTDIR)/usr/bin/$(NAME)

deb: debian/changelog
	DH_VERBOSE=1 dpkg-buildpackage -b

debian/changelog: Makefile
	printf '%s (%s) bullseye bookworm trixie; urgency=medium\n\n' $(NAME) $(VERSION) > $@
	echo "  * just check github" >> $@
	printf -- '\n -- University of Michigan Library IT <lit-noreply@umich.edu>  ' >> $@
	git show --no-patch --format=%cD >> $@
