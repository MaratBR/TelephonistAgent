TAG := release
OUTPUT_FILENAME := telephonist-agent
OUTPUT_PATH := bin/release

all: build

clean:
	@rm -rf ./bin

rebuild: clean build

build:
	go build -o $(OUTPUT_PATH)/$(OUTPUT_FILENAME) -tags $(TAG)		

__set_debug:
	$(eval TAG := debug)
	$(eval OUTPUT_PATH := bin/debug)


build-debug: __set_debug build


run: build-debug
	$(OUTPUT_PATH)/$(OUTPUT_FILENAME) -v --secure

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