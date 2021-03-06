.PHONY: build test clean

build:
	export GO111MODULE=on
	go build -ldflags="-s -w" -o bin/auditr-agent-go .

test:
	ENV_PATH=$(shell pwd)/testdata/dotenv go test -v ./... -timeout 30s

clean:
	rm -rf ./bin ./vendor Gopkg.lock
