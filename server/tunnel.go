package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    "net/url"
    "os/user"
    "path"
    "strconv"
    "sync"
    "time"

    "github.com/hashicorp/yamux"
)

const (
    _CheckIpUrl = "https://checkip.amazonaws.com"
)

type Request struct {
    Host   string `json:"address"`
    Tunnel string `json:"string"`
    Key    string `json:"key"`
    User   string `json:"user"`
}

type TunnelConnection struct {
    Conn_ net.Conn
    Sess_ *yamux.Session
    Time_ time.Time
}

type Tunnel struct {
    SSHAddr_           string
    SSHUser_           string
    SSHKey_            *SSHKey
    LambdaHandler_     *AwsLambda
    TunnelListen_      net.Listener
    ProxyForwarderUrl_ string
    TunnelMutex_       sync.RWMutex
    TunnelConns_       []*TunnelConnection
    ReqNum_            uint64
    ConnTimeoutS_      int64
    Size_              int64
    Running_           bool
}

func (self *Tunnel) SetProxyForwarderUrl(proxyForwarderUrl string) {
    self.ProxyForwarderUrl_ = proxyForwarderUrl
}

func (self *Tunnel) Connect() {
    payload, err := json.Marshal(Request{
        Host:   self.SSHAddr_,
        Tunnel: net.JoinHostPort("localhost", strconv.Itoa(self.TunnelListen_.Addr().(*net.TCPAddr).Port)),
        Key:    self.SSHKey_.GetPrivate(),
        User:   self.SSHUser_,
    })
    if err != nil {
        log.Fatalf("unable to marshal request: %v", err)
    }

    err = self.LambdaHandler_.Invoke(payload)
    if err != nil {
        log.Fatalf("lambda invoke failed: %v", err)
    }
}

func (self *Tunnel) Close() {
    log.Println("Tunnel Invalidate")
    _ = self.SSHKey_.Invalidate()

    self.TunnelMutex_.RLock()
    for _, v := range self.TunnelConns_ {
        log.Println(v.Conn_.RemoteAddr().String() + " close")
        v.Sess_.Close()
    }
    self.TunnelMutex_.RUnlock()

    self.TunnelListen_.Close()
}

func (self *Tunnel) GetLocalPublicIP(proxyURL string) (string, error) {
    var httpClient *http.Client = http.DefaultClient

    if proxyURL != "" {
        proxyUrl, err := url.Parse(proxyURL)
        if err != nil {
            return "", err
        }

        httpClient = &http.Client{
            Transport: &http.Transport{
                Proxy: http.ProxyURL(proxyUrl),
            },
        }
        defer httpClient.CloseIdleConnections()
    }

    resp, err := httpClient.Get(_CheckIpUrl)
    if err != nil {
        return "", err
    }

    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)

    if err != nil {
        return "", err
    }

    return string(bytes.TrimSpace(body)), err
}

func (self *Tunnel) RunConnTrigger() {
    for {
        for {
            if self.Running_ {
                break
            }
            log.Printf("waitting connect...")
            time.Sleep(time.Second)
        }
        log.Printf("trigger lambda %d", self.ConnTimeoutS_)
        go self.Connect()
        time.Sleep(time.Second * time.Duration(self.ConnTimeoutS_))
    }
}

func (self *Tunnel) RunAcceptTunnel() {
    allLambdaIPs := map[string]int{}
    for {
        c, err := self.TunnelListen_.Accept()
        if err != nil {
            log.Println("Failed to accept tunnel connection")
            time.Sleep(time.Second * 5)
            continue
        }
        log.Println("Accepted tunnel connection from", c.RemoteAddr())

        tunnelSession, err := yamux.Client(c, nil)
        if err != nil {
            log.Println("Failed to start session inside tunnel")
            time.Sleep(time.Second * 5)
            continue
        }
        log.Println("Established session to", tunnelSession.RemoteAddr())

        self.TunnelMutex_.Lock()
        conn := &TunnelConnection{
            Conn_: c,
            Sess_: tunnelSession,
            Time_: time.Now(),
        }
        self.TunnelConns_ = append(self.TunnelConns_, conn)
        self.TunnelMutex_.Unlock()

        go self.PingConn(conn)

        externalIP, err := self.GetLocalPublicIP("http://" + self.ProxyForwarderUrl_)
        if err != nil {
            log.Println("Failed to check ip address:", err)
        } else {
            allLambdaIPs[externalIP] += 1
        }

        log.Println("---------------")
        log.Println("Current Lambda IP Address: ", externalIP)
        log.Println("Active Lambda Tunnel Count: ", len(self.TunnelConns_))
        count := 1
        for _, v := range self.TunnelConns_ {
            log.Printf("Lambda Tunnel #%v\n", count)
            log.Println("   Connection ID: " + v.Conn_.RemoteAddr().String())
            log.Println("   Start Time: " + v.Time_.Format("2006-01-02T15:04:05"))
            log.Println("   Active Streams: " + strconv.Itoa(v.Sess_.NumStreams()))
            count++
        }
        log.Printf("%v Unique Lambda IPs Used So Far\n", len(allLambdaIPs))
        log.Println("---------------")
    }
}

func (self *Tunnel) RemoveConn(conn *TunnelConnection, isClose bool) {
    self.TunnelMutex_.Lock()
    for k, v := range self.TunnelConns_ {
        if conn.Conn_.RemoteAddr().String() == v.Conn_.RemoteAddr().String() {
            log.Println("Removing tunnel", conn.Conn_.RemoteAddr().String())
            self.TunnelConns_ = append(self.TunnelConns_[:k], self.TunnelConns_[k+1:]...)
            break
        }
    }
    self.TunnelMutex_.Unlock()

    if isClose {
        log.Println("Close tunnel", conn.Conn_.RemoteAddr().String())
        err := conn.Sess_.Close()
        if err != nil {
            log.Printf("error close connID=%v: %v", conn.Conn_.RemoteAddr().String(), err)
        }
    }
}

func (self *Tunnel) WaitReady() {
    for {
        self.TunnelMutex_.RLock()
        connSize := len(self.TunnelConns_)
        self.TunnelMutex_.RUnlock()

        if connSize != 0 {
            break
        }
        log.Println("wait ready...")
        time.Sleep(time.Second)
    }
}

func (self *Tunnel) GetStream() (*yamux.Stream, error) {
    for {
        self.WaitReady()

        var nowConn *TunnelConnection
        self.TunnelMutex_.RLock()
        if len(self.TunnelConns_) > 0 {
            nowConn = self.TunnelConns_[self.ReqNum_%uint64(len(self.TunnelConns_))]
            self.ReqNum_++
        }
        self.TunnelMutex_.RUnlock()

        if nowConn != nil {
            stream, err := nowConn.Sess_.OpenStream()
            return stream, err
        }

        log.Println("No active tunnel session available. Retrying..")
        time.Sleep(time.Second)
    }
}

func (self *Tunnel) PingConn(conn *TunnelConnection) {
    for {
        _, err := conn.Sess_.Ping()
        if err != nil {
            if time.Since(conn.Time_).Seconds() < float64(self.ConnTimeoutS_-2) {
                log.Println("Close early")
            }
            self.RemoveConn(conn, true)
            break
        }
        if time.Since(conn.Time_).Seconds() > float64(self.ConnTimeoutS_) {
            self.RemoveConn(conn, false)
        }
        time.Sleep(time.Millisecond * 300)
    }
}

func NewTunnel(awslambdaHandler *AwsLambda, size int64, sshPort string, connTimeoutS int64) (*Tunnel, error) {
    var tunnel = new(Tunnel)

    hostIP, err := tunnel.GetLocalPublicIP("")
    if err != nil {
        return nil, fmt.Errorf("cant get ip: %w", err)
    }
    log.Printf("public ip: %s", hostIP)

    curUser, err := user.Current()
    if err != nil {
        return nil, fmt.Errorf("cant get current user: %w", err)
    }
    log.Printf("current user: %s", curUser.Username)

    pk, err := NewSSHKey(path.Join(curUser.HomeDir, ".ssh/authorized_keys"))
    if err != nil {
        return nil, fmt.Errorf("cant initialize ssh key: %w", err)
    }

    // setup tunnel listener on random port
    tunnelListen, err := net.Listen("tcp", "")
    if err != nil {
        return nil, fmt.Errorf("failed to start tunnel: %+v", err)
    }
    log.Printf("tunnel listen: %s", tunnelListen.Addr().String())

    tunnel.TunnelListen_ = tunnelListen
    tunnel.TunnelConns_ = make([]*TunnelConnection, 0)

    tunnel.LambdaHandler_ = awslambdaHandler
    tunnel.SSHAddr_ = net.JoinHostPort(hostIP, "22")
    tunnel.SSHUser_ = curUser.Username
    tunnel.SSHKey_ = pk
    tunnel.ConnTimeoutS_ = connTimeoutS
    tunnel.Size_ = size
    tunnel.ReqNum_ = 0
    tunnel.Running_ = true

    return tunnel, nil
}

func (self *Tunnel) Run() {
    go self.RunAcceptTunnel()

    for i := 0; i < int(self.Size_); i++ {
        log.Println("start tunnel ", i)
        go self.RunConnTrigger()
        time.Sleep(time.Second * 3)
    }
}
