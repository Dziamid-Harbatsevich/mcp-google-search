BINARY := mcp-google-search

.PHONY: build test vet fmt tidy clean run

build:
	go build -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

run: build
	./$(BINARY) -transport stdio
