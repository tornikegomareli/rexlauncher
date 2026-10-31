.PHONY: build test e2e lint clean

build:
	go build -o bin/rex-jump .

test:
	go test -race ./...

# Runs against a Rex server of its own, and needs Rex to be installed.
e2e:
	go test -race -tags e2e -count=1 ./...

lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	go vet ./...

clean:
	rm -rf bin
