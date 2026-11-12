BINARY := runall
BINDIR := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: all install uninstall vet lint clean

all: install

install:
	go install .
	@echo "Installed $(BINDIR)/$(BINARY)"

uninstall:
	rm -f $(BINDIR)/$(BINARY)

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)