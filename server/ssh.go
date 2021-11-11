package main

import (
    "bytes"
    "errors"
    "fmt"
    "log"
    "os"

    "crypto/rand"
    "crypto/rsa"
    "crypto/x509"
    "encoding/pem"

    "golang.org/x/crypto/ssh"
)

type SSHKey struct {
    AuthFile_   string
    PrivateKey_ string
    PublicKey_  []byte
}

func (self *SSHKey) GetPrivate() string {
    return self.PrivateKey_
}

func (self *SSHKey) Invalidate() error {
    f, err := os.ReadFile(self.AuthFile_)
    if err != nil {
        return err
    }
    log.Println("SSHKey Invalidate")

    removed := bytes.ReplaceAll(f, self.PublicKey_, []byte{})

    return os.WriteFile(self.AuthFile_, removed, 0600)
}

func NewSSHKey(authfile string) (*SSHKey, error) {
    private, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        return nil, errors.New("cannot generate key")
    }
    privateBytes := pem.EncodeToMemory(&pem.Block{
        Type:  "RSA PRIVATE KEY",
        Bytes: x509.MarshalPKCS1PrivateKey(private),
    })

    public, err := ssh.NewPublicKey(private.Public())
    if err != nil {
        return nil, errors.New("cannot get public ssh key")
    }
    publicBytes := ssh.MarshalAuthorizedKey(public)

    contents, err := os.ReadFile(authfile)
    if err != nil {
        if !errors.Is(err, os.ErrNotExist) {
            return nil, fmt.Errorf("cannot read authorized keys: %w", err)
        }
    }

    if bytes.Contains(contents, publicBytes) {
        return nil, errors.New("key already exists")
    }

    added := append(contents, publicBytes...)
    err = os.WriteFile(authfile, added, 0644)
    if err != nil {
        return nil, fmt.Errorf("cannot write to authorized keys: %w", err)
    }

    return &SSHKey{
        AuthFile_:   authfile,
        PrivateKey_: string(privateBytes),
        PublicKey_:  publicBytes,
    }, nil
}
