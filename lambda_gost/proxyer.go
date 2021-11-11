package main

import (
    "fmt"
    "log"
    "net"

    "github.com/ginuerzh/gost"
)

type Proxyer struct {
    Port_         string
    ListenerGost_ gost.Listener
    ServerGost_   *gost.Server
}

func (self *Proxyer) Run() {
    ln, err := gost.TCPListener(":0")
    if err != nil {
        log.Fatal(err)
    }
    self.ListenerGost_ = ln
    self.Port_ = fmt.Sprintf(":%v", ln.Addr().(*net.TCPAddr).Port)

    h := gost.AutoHandler()
    s := &gost.Server{Listener: ln}
    self.ServerGost_ = s

    err = s.Serve(h)
    if err != nil {
        log.Printf("Server is now exiting: %v\n", err)
    }
}

func (self *Proxyer) Close() {
    log.Println("Closing down server")
    err := self.ServerGost_.Close()
    if err != nil {
        log.Printf("closing server error: %v\n", err)
    }
    log.Println("Closing down listener")
    err = self.ListenerGost_.Close()
    if err != nil {
        log.Printf("closing listener error: %v\n", err)
    }
}

func NewProxyer() *Proxyer {
    server := &Proxyer{}
    go server.Run()
    return server
}
