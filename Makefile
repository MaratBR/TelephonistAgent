VERSION := 0.2.0
TAG := release
OUTPUT_FILENAME := telephonist-agent
OUTPUT_PATH := bin/release
GHL := $(GOPATH)/bin/github-release
GH_USER ?= maratbr
GH_REPO ?= TelephonistAgent


all: build

clean:
	@rm -rf ./bin

rebuild: clean build

generate:
	go generate ./locales

build: generate
	go build -o $(OUTPUT_PATH)/$(OUTPUT_FILENAME) -tags $(TAG)		

__set_debug:
	$(eval TAG := debug)
	$(eval OUTPUT_PATH := bin/debug)


build-debug: __set_debug build

install-service: build
	sudo $(OUTPUT_PATH)/$(OUTPUT_FILENAME) service install -f
	sudo systemctl status telephonist-agent.service


run: build-debug
	sudo $(OUTPUT_PATH)/$(OUTPUT_FILENAME) -v 

test:
	@go test 
	@go test ./telephonist
	@go test ./taskscheduler
	@go test ./utils

chown-directories:
	sudo mkdir -p /var/telephonist-agent
	sudo mkdir -p /etc/telephonist-agent
	sudo mkdir -p /var/log/telephonist-agent

	sudo chown -R $(USER) /var/telephonist-agent
	sudo chown -R $(USER) /var/log/telephonist-agent
	sudo chown -R $(USER) /etc/telephonist-agent

github-release: build
	cp $(OUTPUT_PATH)/$(OUTPUT_FILENAME) "$(OUTPUT_PATH)/$(OUTPUT_FILENAME)-$(VERSION)"
	$(GHL) release \
		--user $(GH_USER) \
		--repo $(GH_REPO) \
		--tag v$(VERSION) \
		--name "Telephonist Agent v$(VERSION)" \
		--description "Automatic release for version $(VERSION)"
	sleep 1
	$(GHL) upload \
		--user $(GH_USER) \
		--repo $(GH_REPO) \
		--tag v$(VERSION) \
		--name "$(OUTPUT_FILENAME)-$(VERSION)" \
		--file "$(OUTPUT_PATH)/$(OUTPUT_FILENAME)-$(VERSION)"