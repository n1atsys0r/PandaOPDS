.PHONY: all webui build

all: webui build

webui:
	cd frontend && npm run build

build:
	go build ./cmd/pandaopds
