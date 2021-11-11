package main

import (
    "errors"
    "fmt"
    "io"
    "log"
    "net"
    "sync"
    "time"

    "github.com/ginuerzh/gost"
)

type Proxyer struct {
    ListenerUrl_       string
    Tunnel_            *Tunnel
    ForwarderListener_ net.Listener
    GostServerHandler_ *gost.Server
    LastReqTime_       time.Time
    CheckFailCount_    int
}

func NewProxyer(listenerUrl string, tunnel *Tunnel) (*Proxyer, error) {
    var proxy = new(Proxyer)
    proxy.ListenerUrl_ = listenerUrl
    proxy.Tunnel_ = tunnel

    err := proxy.RunForwarder()
    if err != nil {
        return nil, fmt.Errorf("proxy.RunForwarder: %+v", err)
    }

    err = proxy.RunProxy()
    if err != nil {
        return nil, fmt.Errorf("proxy.RunProxy: %+v", err)
    }

    return proxy, nil
}

func (self *Proxyer) IOCopy(src io.ReadWriteCloser, dst io.ReadWriteCloser) {
    defer dst.Close()
    defer src.Close()

    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        _, _ = io.Copy(dst, src)
        dst.Close()
        wg.Done()
    }()

    wg.Add(1)
    go func() {
        _, _ = io.Copy(src, dst)
        src.Close()
        wg.Done()
    }()
    wg.Wait()
}

func (self *Proxyer) ServeForward() {
    for {
        c, err := self.ForwarderListener_.Accept()
        if err != nil {
            if !errors.Is(err, net.ErrClosed) {
                log.Printf("unable to accept forward request: %+v", err)
            }
            return
        }

        if !self.Tunnel_.Running_ {
            self.Tunnel_.Running_ = true
            self.CheckFailCount_ = 0
        }
        if time.Now().Sub(self.LastReqTime_) >= time.Second*time.Duration(self.Tunnel_.ConnTimeoutS_-5) {
            self.CheckFailCount_++
            if self.CheckFailCount_ > 3 {
                self.Tunnel_.Running_ = false
                log.Printf("Stop tunnel...", self.CheckFailCount_)
                self.CheckFailCount_ = 0
            }
        } else {
            self.Tunnel_.Running_ = true
            self.CheckFailCount_ = 0
        }

        self.LastReqTime_ = time.Now()

        // handle request
        go func(c net.Conn) {
            stream, err := self.Tunnel_.GetStream()
            if err != nil {
                log.Printf("unable to open tunnel stream: %+v", err)
                return
            }

            self.IOCopy(c, stream)
        }(c)
    }
}

func (self *Proxyer) RunForwarder() error {
    forwarder, err := net.Listen("tcp", "")
    if err != nil {
        return fmt.Errorf("failed to start proxyer: %+v", err)
    }
    self.ForwarderListener_ = forwarder

    go self.ServeForward()

    return nil
}

func (self *Proxyer) _GetGostChain(forwarderUrl string) (*gost.Chain, error) {
    node, err := gost.ParseNode(forwarderUrl)
    if err != nil {
        return nil, fmt.Errorf("gost.ParseNode: %+v", err)
    }
    chain := gost.NewChain()
    chain.Retries = 0
    ngroup := gost.NewNodeGroup()
    ngroup.ID = 1
    tr := gost.TCPTransporter()
    connector := gost.AutoConnector(node.User)
    host := node.Get("host")
    if host == "" {
        host = node.Host
    }
    timeout := node.GetDuration("timeout")
    node.DialOptions = append(node.DialOptions,
        gost.TimeoutDialOption(timeout),
    )
    node.ConnectOptions = []gost.ConnectOption{
        gost.UserAgentConnectOption(node.Get("agent")),
        gost.NoTLSConnectOption(node.GetBool("notls")),
        gost.NoDelayConnectOption(node.GetBool("nodelay")),
    }
    handshakeOptions := []gost.HandshakeOption{
        gost.AddrHandshakeOption(node.Addr),
        gost.HostHandshakeOption(host),
        gost.UserHandshakeOption(node.User),
        gost.IntervalHandshakeOption(node.GetDuration("ping")),
        gost.TimeoutHandshakeOption(timeout),
        gost.RetryHandshakeOption(node.GetInt("retry")),
    }
    node.HandshakeOptions = handshakeOptions

    node.Client = &gost.Client{
        Connector:   connector,
        Transporter: tr,
    }
    node.ID = 1
    ngroup.AddNode(node)
    ngroup.SetSelector(nil,
        gost.WithFilter(
            &gost.FailFilter{
                MaxFails:    node.GetInt("max_fails"),
                FailTimeout: node.GetDuration("fail_timeout"),
            },
            &gost.InvalidFilter{},
        ),
        gost.WithStrategy(gost.NewStrategy(node.Get("strategy"))),
    )
    chain.AddNodeGroup(ngroup)

    return chain, nil
}

func (self *Proxyer) RunProxy() error {
    chain, err := self._GetGostChain(self.ForwarderListener_.Addr().String())
    if err != nil {
        return fmt.Errorf("_GetGostChain: %+v", err)
    }

    node, err := gost.ParseNode(self.ListenerUrl_)
    if err != nil {
        return fmt.Errorf("gost.ParseNode: %+v", err)
    }
    ln, err := gost.TCPListener(node.Addr)
    if err != nil {
        return fmt.Errorf("gost.TCPListener: %+v", err)
    }

    handler := gost.AutoHandler()

    handler.Init(
        gost.AddrHandlerOption(ln.Addr().String()),
        gost.ChainHandlerOption(chain),
        gost.UsersHandlerOption(node.User),
    )

    self.GostServerHandler_ = &gost.Server{Listener: ln}
    go self.GostServerHandler_.Serve(handler)

    return nil
}

func (self *Proxyer) Close() {
    self.ForwarderListener_.Close()
    self.GostServerHandler_.Close()
}
