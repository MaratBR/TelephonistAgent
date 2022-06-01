VERSION := 0.3.3
TAG := release
OUTPUT_FILENAME := telephonist-agent
OUTPUT_PATH := bin/release
GH_USER ?= maratbr
GH_REPO ?= TelephonistAgent

all: build

init:
	go get github.com/ChimeraCoder/gojson/gojson
	go install github.com/ChimeraCoder/gojson/gojson
	go mod download
	go install github.com/volatiletech/sqlboiler/v4@latest
	go install github.com/volatiletech/sqlboiler/v4/drivers/sqlboiler-sqlite3

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
	gh release create v$(VERSION) --generate-notes '$(OUTPUT_PATH)/$(OUTPUT_FILENAME)#CLI app'

chown-dirs: 
	sudo chown -R marat /etc/telephonist-agent
	sudo chmod 777 /etc/telephonist-agent

database:
	rm -f ./test_database.sqlite3
	sqlite3 test_database.sqlite3 <./db/schema.sql
	reform-db -db-driver=sqlite3 -db-source=test_database.sqlite3 init db
	reform db