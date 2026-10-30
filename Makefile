BINARY := runall
BINDIR := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: all build install uninstall vet clean

all: install

build:
	go build -o $(BINARY) .

install: build
	go install .
	@echo "Installed $(BINDIR)/$(BINARY)"

uninstall:
	rm -f $(BINDIR)/$(BINARY)

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
