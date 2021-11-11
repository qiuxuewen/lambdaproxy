# HTTP proxy on AWS Lambda                                                                                                                 

This is basically a reimplementation of [dan-v/awslambdaproxy](https://github.com/dan-v/awslambdaproxy) 

## Usage
1. `git clone https://github.com/qiuxuewen/lambdaproxy`
2. `cd lambdaproxy`
3. `make`
```shell
export AWS_ACCESS_KEY_ID=XXXXXX
export AWS_SECRET_ACCESS_KEY=XXXXXX
./bin/lambdaproxy -r us-west-1 -l test:testpwd@:8080
