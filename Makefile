.PHONY: \
    lambda \
    server

all: clean lambda server

clean:
	rm -Rf bin
	rm -f server/bindata.go

lambda:
	#CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/lambda/main ./lambda
	CGO_ENABLED=0 go build -o bin/lambda/main ./lambda
	zip -jr bin/lambda bin/lambda
	go-bindata -nocompress -pkg main -o server/bindata.go bin/lambda.zip

server:
	#CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/lambdaproxy ./server
	CGO_ENABLED=0 go build -o bin/lambdaproxy ./server


