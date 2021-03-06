package main

import (
    "log"
    "net/http"
    "time"

    "github.com/aws/aws-lambda-go/lambda"
    "github.com/elazarl/goproxy"
    "github.com/hashicorp/yamux"
    "golang.org/x/crypto/ssh"
)

type Request struct {
    Host   string `json:"address"`
    Tunnel string `json:"string"`
    Key    string `json:"key"`
    User   string `json:"user"`
}

func ConnectSSH(host, user, key string) (*ssh.Client, error) {
    signer, err := ssh.ParsePrivateKey([]byte(key))
    if err != nil {
        return nil, err
    }
    return ssh.Dial("tcp", host, &ssh.ClientConfig{
        User:            user,
        HostKeyCallback: ssh.InsecureIgnoreHostKey(),
        Auth: []ssh.AuthMethod{
            ssh.PublicKeys(signer),
        },
    })
}

func GetTunnel(client *ssh.Client, tunnel string) (*yamux.Session, error) {
    service, err := client.Dial("tcp", tunnel)
    if err != nil {
        return nil, err
    }

    return yamux.Server(service, nil)
}

func HandleRequest(req Request) error {
    log.Printf("new proxy request, connecting to %s", req.Host)
    client, err := ConnectSSH(req.Host, req.User, req.Key)
    if err != nil {
        return err
    }
    defer client.Close()

    log.Printf("establishing tunnel on %s", req.Tunnel)
    tunnel, err := GetTunnel(client, req.Tunnel)
    if err != nil {
        return err
    }
    defer tunnel.Close()

    log.Println("starting proxy server")
    startTime := time.Now()

    defer func() {
        runtime := time.Since(startTime).String()
        log.Printf("closing proxy server after %s", runtime)
    }()
    return http.Serve(tunnel, goproxy.NewProxyHttpServer())
}

func main() {
    lambda.Start(HandleRequest)
}
