.PHONY: build test clean

build:
	export GO111MODULE=on
	go build -ldflags="-s -w" -o bin/auditr-agent-go .

test:
	CONFIG=$(shell pwd)/testdata/dotenv go test -v ./...

clean:
	rm -rf ./bin ./vendor Gopkg.lock
