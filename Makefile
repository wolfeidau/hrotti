NAME=simple_broker
ARCH=$(shell uname -m)
VERSION=1.1.0
HASH=$(shell git rev-parse --short HEAD)

build:
	mkdir -p build/Linux  && GOOS=linux  go build -ldflags "-X main.Version=$(VERSION)" -o build/Linux/$(NAME) ./cmds/simple_broker
	mkdir -p build/Darwin && GOOS=darwin go build -ldflags "-X main.Version=$(VERSION)" -o build/Darwin/$(NAME) ./cmds/simple_broker
	mkdir -p build/Windows && GOOS=windows go build -ldflags "-X main.Version=$(VERSION)" -o build/Windows/$(NAME).exe ./cmds/simple_broker

test:
	go test ./...

docker-build: build
	docker build -t simple_broker:${HASH} .
	docker tag -f simple_broker:${HASH} simple_broker:latest
	docker tag -f simple_broker:${HASH} gcr.io/cohort-staging/simple_broker:${HASH}

docker-push:
	gcloud docker push gcr.io/cohort-staging/simple_broker:${HASH}

release: build
	rm -rf release && mkdir release
	tar -zcf release/$(NAME)_$(VERSION)_linux_$(ARCH).tgz -C build/Linux $(NAME)
	tar -zcf release/$(NAME)_$(VERSION)_darwin_$(ARCH).tgz -C build/Darwin $(NAME)
	tar -zcf release/$(NAME)_$(VERSION)_windows_$(ARCH).tgz -C build/Windows $(NAME)
	gh-release create wolfeidau/$(NAME) $(VERSION) $(shell git rev-parse --abbrev-ref HEAD)

run:
	go run cmds/simple_broker/main.go

.PHONY: build test release
