all: binary

binary:
	go build -o bin/telephonist-cli	

run: binary
	sudo bin/telephonist-cli -v --secure