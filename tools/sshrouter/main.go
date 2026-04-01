// SSH Router - Routes SSH connections based on username to target hosts
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

type Config struct {
	Routes         map[string]string `json:"routes"`
	Default        string            `json:"default,omitempty"`
	ListenAddr     string            `json:"listen_addr,omitempty"`
	HostKeyPath    string            `json:"host_key_path,omitempty"`
	PrivateKeyPath string            `json:"private_key_path,omitempty"`
}

func main() {
	configPath := flag.String("config", "/etc/sshrouter/routes.json", "config file")
	flag.Parse()

	data, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("failed to read config file: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":2222"
	}
	if cfg.HostKeyPath == "" {
		cfg.HostKeyPath = "/etc/ssh/ssh_host_ed25519_key"
	}
	if cfg.PrivateKeyPath == "" {
		cfg.PrivateKeyPath = "/etc/ssh/ssh_host_ed25519_key"
	}

	hostKeyBytes, err := os.ReadFile(cfg.HostKeyPath)
	if err != nil {
		log.Fatalf("failed to read host key: %v", err)
	}
	hostKey, err := ssh.ParsePrivateKey(hostKeyBytes)
	if err != nil {
		log.Fatalf("failed to parse host key: %v", err)
	}

	privateKeyBytes, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		log.Fatalf("failed to read private key for outbound connections: %v", err)
	}
	outboundSigner, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		log.Fatalf("failed to parse private key for outbound connections: %v", err)
	}

	sshConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
		ServerVersion: "SSH-2.0-sshrouter",
	}
	sshConfig.AddHostKey(hostKey)

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", cfg.ListenAddr, err)
	}
	log.Printf("SSH Router listening on %s", cfg.ListenAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("failed to accept connection: %v", err)
			continue
		}
		go handleConn(conn, sshConfig, &cfg, outboundSigner)
	}
}

func handleConn(conn net.Conn, sshConfig *ssh.ServerConfig, cfg *Config, signer ssh.Signer) {
	defer conn.Close()
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, sshConfig)
	if err != nil {
		log.Printf("SSH handshake failed: %v", err)
		return
	}
	defer serverConn.Close()

	incomingUser := serverConn.User()
	route, ok := getTarget(incomingUser, cfg)
	if !ok {
		log.Printf("no route for user %q", incomingUser)
		return
	}
	log.Printf("routing %s to %s", incomingUser, route)

	// Parse the route string: [user@]host[:port]
	var targetUser, targetHost, targetPort string

	// Split user
	if parts := strings.SplitN(route, "@", 2); len(parts) == 2 {
		targetUser = parts[0]
		route = parts[1]
	} else {
		targetUser = incomingUser
	}

	// Split host and port
	if parts := strings.SplitN(route, ":", 2); len(parts) == 2 {
		targetHost = parts[0]
		targetPort = parts[1]
	} else {
		targetHost = route
		targetPort = "22" // default ssh port
	}

	targetAddr := net.JoinHostPort(targetHost, targetPort)

	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("failed to dial target %s: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	clientConfig := &ssh.ClientConfig{
		User:            targetUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	clientSshConn, clientChans, clientReqs, err := ssh.NewClientConn(targetConn, targetAddr, clientConfig)
	if err != nil {
		log.Printf("failed to establish SSH connection to target %s: %v", targetAddr, err)
		return
	}
	defer clientSshConn.Close()

	go forwardRequests(clientSshConn, reqs)
	go forwardRequests(serverConn, clientReqs)
	go forwardChannels(clientSshConn, chans)
	go forwardChannels(serverConn, clientChans)

	serverConn.Wait()
}

func getTarget(user string, cfg *Config) (string, bool) {
	if target, ok := cfg.Routes[user]; ok {
		return target, true
	}
	for pattern, target := range cfg.Routes {
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(user, prefix) {
				return target, true
			}
		}
	}
	if cfg.Default != "" {
		return cfg.Default, true
	}
	return "", false
}

func forwardRequests(dst ssh.Conn, src <-chan *ssh.Request) {
	for req := range src {
		ok, payload, err := dst.SendRequest(req.Type, req.WantReply, req.Payload)
		if err != nil {
			return
		}
		if req.WantReply {
			req.Reply(ok, payload)
		}
	}
}

func forwardChannels(dst ssh.Conn, src <-chan ssh.NewChannel) {
	for newChannel := range src {
		go handleChannel(dst, newChannel)
	}
}

func handleChannel(dst ssh.Conn, newChannel ssh.NewChannel) {
	dstChannel, dstRequests, err := dst.OpenChannel(newChannel.ChannelType(), newChannel.ExtraData())
	if err != nil {
		if openErr, ok := err.(*ssh.OpenChannelError); ok {
			newChannel.Reject(openErr.Reason, openErr.Message)
		} else {
			newChannel.Reject(ssh.ConnectionFailed, "failed to open channel")
		}
		return
	}

	srcChannel, srcRequests, err := newChannel.Accept()
	if err != nil {
		dstChannel.Close()
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(dstChannel, srcChannel)
		dstChannel.CloseWrite()
	}()
	go func() {
		defer wg.Done()
		io.Copy(srcChannel, dstChannel)
		srcChannel.CloseWrite()
	}()

	go forwardChannelRequests(dstChannel, srcRequests)
	go forwardChannelRequests(srcChannel, dstRequests)

	wg.Wait()
	srcChannel.Close()
	dstChannel.Close()
}

func forwardChannelRequests(dst ssh.Channel, src <-chan *ssh.Request) {
	for req := range src {
		ok, err := dst.SendRequest(req.Type, req.WantReply, req.Payload)
		if err != nil {
			return
		}
		if req.WantReply {
			req.Reply(ok, nil)
		}
	}
}