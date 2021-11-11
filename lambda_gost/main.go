package main

import (
    "io"
    "log"
    "net"
    "sync"
    "time"

    "github.com/aws/aws-lambda-go/lambda"
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

func BidirectionalCopy(src io.ReadWriteCloser, dst io.ReadWriteCloser) {
    defer dst.Close()
    defer src.Close()

    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        _, err := io.Copy(dst, src)
        dst.Close()
        if err != nil {
            log.Printf("io copy dst to src: %+v", err)
        }
        wg.Done()
    }()

    wg.Add(1)
    go func() {
        _, err := io.Copy(src, dst)
        src.Close()
        if err != nil {
            log.Printf("io copy src to dst: %+v", err)
        }
        wg.Done()
    }()
    wg.Wait()
}

func CopyData(proxyer *Proxyer, tunnelSess *yamux.Session) {
    for {
        proxySocketConn, proxySocketErr := net.Dial("tcp", proxyer.Port_)
        if proxySocketErr != nil {
            log.Printf("Failed to open connection to proxy: %v\n", proxySocketErr)
            time.Sleep(time.Second)
            continue
        }
        log.Printf("Opened local connection to proxy on port %v\n", proxyer.Port_)

        tunnelStream, tunnelErr := tunnelSess.Accept()
        if tunnelErr != nil {
            log.Printf("Failed to start new stream: %v. Exiting function.\n", tunnelErr)
            return
        }
        log.Println("Started new stream")

        go BidirectionalCopy(tunnelStream, proxySocketConn)
    }
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

    lambdaProxyer := NewProxyer()
    defer lambdaProxyer.Close()

    log.Println("starting proxy server")
    startTime := time.Now()

    defer func() {
        runtime := time.Since(startTime).String()
        log.Printf("closing proxy server after %s", runtime)
    }()
    CopyData(lambdaProxyer, tunnel)

    return nil
}

func main() {
    lambda.Start(HandleRequest)
}
