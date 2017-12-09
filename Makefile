.PHONY: all build bash stop clean

all: build

build:
	docker build -t bitmesh-chat .

bash:
	docker run -it bitmesh-chat /bin/bash

stop:
	@docker stop $(shell docker ps -aq)

clean:
	@docker rm $(shell docker ps -qa --no-trunc --filter "status=exited")