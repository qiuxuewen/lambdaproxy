package main

import (
    "flag"
    "log"
    "net"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "syscall"
)

var (
    __ListenerUrl      = flag.String("l", "admin:awslambdaproxy@:8080", "[scheme://][user:pass@host]:port, https://github.com/ginuerzh/gost/blob/master/README_en.md#getting-started.")
    __SSHPort          = flag.String("p", "22", "ssh port")
    __Regions          = flag.String("r", "us-west-2", "aws regions,us-east-1,us-east-2,us-west-1,us-west-2,af-south-1,ap-east-1,ap-south-1,ap-northeast-3,ap-northeast-2,ap-southeast-1,ap-southeast-2,ap-northeast-1,ca-central-1,eu-central-1,eu-west-1,eu-west-2,eu-south-1,eu-west-3,eu-north-1,me-south-1,sa-east-1")
    __AwsIamRoleName   = flag.String("role", "awslambdaproxy-role", "aws iam role name")
    __LambdaName       = flag.String("n", "lambdaproxy", "aws lambda name")
    __LambdaIntervalS  = flag.Int64("f", 60, "run lambda interval seconds")
    __LambdaMemorySize = flag.Int64("m", 256, "lambda memory size")
    __TunnelSize       = flag.Int64("s", 1, "tunnel size")
)

func main() {
    flag.Parse()

    lambdaTimeoutS := *__LambdaIntervalS + 20

    regions := strings.Split(*__Regions, ",")
    awsLambda, err := NewAwsLambda(*__LambdaName, *__AwsIamRoleName, regions, lambdaTimeoutS, *__LambdaMemorySize)
    if err != nil {
        log.Fatalf("unable to new AwsLambda: %+v", err)
    }

    tunnel, err := NewTunnel(awsLambda, *__TunnelSize, *__SSHPort, *__LambdaIntervalS)
    if err != nil {
        log.Fatalf("unable to setup tunneler: %+v", err)
    }
    defer tunnel.Close()

    proxyer, err := NewProxyer(*__ListenerUrl, tunnel)
    if err != nil {
        log.Fatalf("failed to start proxyer: %+v", err)
    }
    defer proxyer.Close()

    tunnel.SetProxyForwarderUrl(net.JoinHostPort("localhost", strconv.Itoa(proxyer.ForwarderListener_.Addr().(*net.TCPAddr).Port)))
    tunnel.Run()

    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)

    <-c
    log.Println("received interrupt, stopping proxy")
}
