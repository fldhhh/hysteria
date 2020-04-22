package main

import (
	"crypto/tls"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/congestion"
	hyCongestion "github.com/tobyxdd/hysteria/pkg/congestion"
	"github.com/tobyxdd/hysteria/pkg/core"
	"io"
	"log"
	"net"
)

func proxyServer(args []string) {
	var config proxyServerConfig
	err := loadConfig(&config, args)
	if err != nil {
		log.Fatalln("Unable to load configuration:", err)
	}
	if err := config.Check(); err != nil {
		log.Fatalln("Configuration error:", err.Error())
	}
	log.Printf("Configuration loaded: %+v\n", config)
	// Load cert
	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		log.Fatalln("Unable to load the certificate:", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{proxyTLSProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	quicConfig := &quic.Config{
		MaxReceiveStreamFlowControlWindow:     config.ReceiveWindowConn,
		MaxReceiveConnectionFlowControlWindow: config.ReceiveWindowClient,
		MaxIncomingStreams:                    config.MaxConnClient,
		KeepAlive:                             true,
	}
	if quicConfig.MaxReceiveStreamFlowControlWindow == 0 {
		quicConfig.MaxReceiveStreamFlowControlWindow = DefaultMaxReceiveStreamFlowControlWindow
	}
	if quicConfig.MaxReceiveConnectionFlowControlWindow == 0 {
		quicConfig.MaxReceiveConnectionFlowControlWindow = DefaultMaxReceiveConnectionFlowControlWindow
	}
	if quicConfig.MaxIncomingStreams == 0 {
		quicConfig.MaxIncomingStreams = DefaultMaxIncomingStreams
	}

	server, err := core.NewServer(config.ListenAddr, tlsConfig, quicConfig,
		uint64(config.UpMbps)*mbpsToBps, uint64(config.DownMbps)*mbpsToBps,
		func(refBPS uint64) congestion.SendAlgorithmWithDebugInfos {
			return hyCongestion.NewBrutalSender(congestion.ByteCount(refBPS))
		},
		func(addr net.Addr, username string, password string, sSend uint64, sRecv uint64) (core.AuthResult, string) {
			// No authentication logic in relay, just log username and speed
			log.Printf("%s (%s) connected, negotiated speed (Mbps): Up %d / Down %d\n",
				addr.String(), username, sSend/mbpsToBps, sRecv/mbpsToBps)
			return core.AuthSuccess, ""
		},
		func(addr net.Addr, username string, err error) {
			log.Printf("%s (%s) disconnected: %s\n", addr.String(), username, err.Error())
		},
		func(addr net.Addr, username string, id int, packet bool, reqAddr string) (core.ConnectResult, string, io.ReadWriteCloser) {
			if !packet {
				// TCP
				log.Printf("%s (%s): [TCP] %s\n", addr.String(), username, reqAddr)
				conn, err := net.Dial("tcp", reqAddr)
				if err != nil {
					log.Printf("TCP error %s: %s\n", reqAddr, err.Error())
					return core.ConnFailed, err.Error(), nil
				}
				return core.ConnSuccess, "", conn
			} else {
				// UDP
				log.Printf("%s (%s): [UDP] %s\n", addr.String(), username, reqAddr)
				conn, err := net.Dial("udp", reqAddr)
				if err != nil {
					log.Printf("UDP error %s: %s\n", reqAddr, err.Error())
					return core.ConnFailed, err.Error(), nil
				}
				return core.ConnSuccess, "", conn
			}
		},
		func(addr net.Addr, username string, id int, packet bool, reqAddr string, err error) {
			if !packet {
				log.Printf("%s (%s): closed [TCP] %s: %s\n", addr.String(), username, reqAddr, err.Error())
			} else {
				log.Printf("%s (%s): closed [UDP] %s: %s\n", addr.String(), username, reqAddr, err.Error())
			}
		},
	)
	if err != nil {
		log.Fatalln("Server initialization failed:", err)
	}
	defer server.Close()
	log.Println("Up and running on", config.ListenAddr)

	log.Fatalln(server.Serve())
}
