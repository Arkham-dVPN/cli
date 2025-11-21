package node

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

const (
	ProtocolStream = "/arkham/vpn/1.0.0"
	ProtocolMDNS   = "arkham-vpn-local"
	ProtocolDHT    = "arkham-vpn-global"
	ProtocolPing   = "/arkham/ping/1.0.0"
)

// PeerInfo holds detailed information about a discovered peer for the API
type PeerInfo struct {
	ID      string   `json:"id"`
	Addrs   []string `json:"addrs"`
	Latency int64    `json:"latency"` // Latency in milliseconds
}

type P2PNode struct {
	mu        sync.Mutex
	host      host.Host
	dht       *kaddht.IpfsDHT
	mdns      mdns.Service
	IsRunning bool
}

func NewP2PNode() *P2PNode {
	return &P2PNode{}
}

func (n *P2PNode) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.IsRunning {
		return nil
	}

	h, err := libp2p.New(libp2p.EnableRelay(), libp2p.EnableHolePunching())
	if err != nil {
		return err
	}
	n.host = h

	// Set stream handlers
	h.SetStreamHandler(ProtocolStream, n.streamHandler)
	h.SetStreamHandler(ProtocolPing, pingHandler)

	if err := n.setupDiscovery(); err != nil {
		h.Close()
		return err
	}

	n.IsRunning = true
	log.Println("P2P Node started. Peer ID:", h.ID().String())
	return nil
}

func (n *P2PNode) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.IsRunning {
		return nil
	}

	if n.mdns != nil {
		n.mdns.Close()
	}
	if n.dht != nil {
		n.dht.Close()
	}
	if n.host != nil {
		if err := n.host.Close(); err != nil {
			return err
		}
	}

	n.IsRunning = false
	log.Println("P2P Node stopped.")
	return nil
}

type NodeStatus struct {
	IsRunning bool     `json:"isRunning"`
	PeerID    string   `json:"peerId,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
}

func (n *P2PNode) Status() NodeStatus {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.IsRunning || n.host == nil {
		return NodeStatus{IsRunning: false}
	}

	addrs := make([]string, 0, len(n.host.Addrs()))
	for _, addr := range n.host.Addrs() {
		addrs = append(addrs, addr.String())
	}

	return NodeStatus{
		IsRunning: true,
		PeerID:    n.host.ID().String(),
		Addresses: addrs,
	}
}

func (n *P2PNode) GetHost() host.Host {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.host
}

func (n *P2PNode) streamHandler(s network.Stream) {
	log.Printf("[WARDEN] Received VPN request from Seeker: %s", s.Conn().RemotePeer())
	s.Close()
}

func pingHandler(s network.Stream) {
	defer s.Close()
	buf := make([]byte, 1)
	_, _ = s.Read(buf)
}

func measureLatency(ctx context.Context, h host.Host, p peer.ID) {
	start := time.Now()
	s, err := h.NewStream(ctx, p, ProtocolPing)
	if err != nil {
		h.Peerstore().Put(p, "latency", int64(9999))
		return
	}
	defer s.Close()

	_, err = s.Write([]byte("p"))
	if err != nil {
		h.Peerstore().Put(p, "latency", int64(9999))
		return
	}

	buf := make([]byte, 1)
	_, err = s.Read(buf)
	if err != nil {
		// Error reading is fine, the stream might be closed already.
	}

	latency := time.Since(start).Milliseconds()
	h.Peerstore().Put(p, "latency", latency)
	log.Printf("Measured latency to %s: %dms", p.String(), latency)
}

func (n *P2PNode) setupDiscovery() error {
	ctx := context.Background()
	if n.host == nil {
		return nil
	}

	mdnsService := mdns.NewMdnsService(n.host, ProtocolMDNS, &discoveryNotifee{h: n.host})
	if err := mdnsService.Start(); err != nil {
		return err
	}
	n.mdns = mdnsService

	kdht, err := kaddht.New(ctx, n.host)
	if err != nil {
		return err
	}
	n.dht = kdht

	if err = kdht.Bootstrap(ctx); err != nil {
		return err
	}

	routingDiscovery := routing.NewRoutingDiscovery(kdht)
	util.Advertise(ctx, routingDiscovery, ProtocolDHT)

	go func() {
		for {
			if !n.IsRunning {
				return
			}
			peers, err := routingDiscovery.FindPeers(ctx, ProtocolDHT)
			if err != nil {
				log.Printf("DHT FindPeers error: %v", err)
				time.Sleep(1 * time.Minute)
				continue
			}
			for p := range peers {
				if p.ID == n.host.ID() {
					continue
				}
				if n.host.Network().Connectedness(p.ID) != network.Connected {
					log.Printf("Connecting to peer found via DHT: %s", p.ID)
					if err := n.host.Connect(ctx, p); err != nil {
						log.Printf("Failed to connect to %s: %v", p.ID, err)
					} else {
						go measureLatency(context.Background(), n.host, p.ID)
					}
				}
			}
			time.Sleep(1 * time.Minute)
		}
	}()

	return nil
}

type discoveryNotifee struct {
	h host.Host
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.h.ID() {
		return
	}
	log.Printf("Found peer via mDNS: %s", pi.ID.String())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := n.h.Connect(ctx, pi); err != nil {
		log.Printf("Failed to connect to mDNS peer %s: %v", pi.ID, err)
	} else {
		go measureLatency(context.Background(), n.h, pi.ID)
	}
}
