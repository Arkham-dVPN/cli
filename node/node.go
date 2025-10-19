package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

const (
	ProtocolMDNS       = "arkham-vpn-local"
	ProtocolDHT        = "arkham-vpn-global"
	ProtocolStream     = "/arkham/vpn/1.0.0"
	ProtocolPing       = "/arkham/ping/1.0.0"
	WireGuardInterface = "wg0"
)

// --- Data Structures --- //

type ConnectRequest struct {
	Hops int `json:"hops"`
}

type VPNRequest struct {
	SeekerPublicKey string   `json:"seeker_public_key"`
	Hops            []string `json:"hops,omitempty"`
}

type VPNResponse struct {
	WardenPublicKey string `json:"warden_public_key"`
}

type APIResponse struct {
	Status          string   `json:"status"`
	Message         string   `json:"message"`
	WardenPeerID    string   `json:"warden_peer_id,omitempty"`
	SeekerPublicKey string   `json:"seeker_public_key,omitempty"`
	WardenPublicKey string   `json:"warden_public_key,omitempty"`
	Path            []string `json:"path,omitempty"`
}

// PeerInfo holds detailed information about a discovered peer for the API
type PeerInfo struct {
	ID      string   `json:"id"`
	Addrs   []string `json:"addrs"`
	Latency int64    `json:"latency"` // Latency in milliseconds
}

// --- P2P Logic --- //

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

// Warden handles the peer-to-peer logic for serving VPN connections.
type Warden struct {
	host host.Host
}

func (w *Warden) streamHandler(s network.Stream) {
	remotePeer := s.Conn().RemotePeer()
	log.Printf("[WARDEN] Received VPN request from Seeker: %s", remotePeer)

	var req VPNRequest
	if err := json.NewDecoder(s).Decode(&req); err != nil {
		log.Printf("[WARDEN] Failed to decode request: %v", err)
		_ = s.Reset()
		return
	}
	defer s.Close()

	// Check if this is an intermediate hop or an exit node
	if len(req.Hops) > 0 {
		// Intermediate hop logic
		nextHopID := req.Hops[0]
		remainingHops := req.Hops[1:]

		nextHop, err := peer.Decode(nextHopID)
		if err != nil {
			log.Printf("[WARDEN] Invalid next hop peer ID: %v", err)
			return
		}

		log.Printf("[WARDEN] Acting as intermediate hop, forwarding request to %s", nextHop)

		// Forward the request to the next hop
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		nextStream, err := w.host.NewStream(ctx, nextHop, ProtocolStream)
		if err != nil {
			log.Printf("[WARDEN] Failed to open stream to next hop: %v", err)
			return
		}

		forwardReq := VPNRequest{
			SeekerPublicKey: req.SeekerPublicKey,
			Hops:            remainingHops,
		}

		if err := json.NewEncoder(nextStream).Encode(forwardReq); err != nil {
			log.Printf("[WARDEN] Failed to forward request: %v", err)
			return
		}

		// Wait for response and forward back
		var resp VPNResponse
		if err := json.NewDecoder(nextStream).Decode(&resp); err != nil {
			log.Printf("[WARDEN] Failed to get response from next hop: %v", err)
			return
		}

		if err := json.NewEncoder(s).Encode(resp); err != nil {
			log.Printf("[WARDEN] Failed to forward response back: %v", err)
			return
		}
		log.Printf("✅ [WARDEN] Intermediate hop for %s -> %s complete.", remotePeer, nextHop)

	} else {
		// Exit node logic
		log.Println("[WARDEN] Acting as exit node.")
		wardenPrivKey, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			log.Printf("[WARDEN] Failed to generate key: %v", err)
			return
		}

		resp := VPNResponse{WardenPublicKey: wardenPrivKey.PublicKey().String()}
		if err := json.NewEncoder(s).Encode(resp); err != nil {
			log.Printf("[WARDEN] Failed to send response: %v", err)
			return
		}
		log.Printf("✅ [WARDEN] Exit node negotiated tunnel for Seeker %s!", remotePeer)
	}
}

// --- API Handlers --- //

func peersHandler(w http.ResponseWriter, r *http.Request, h host.Host) {
	peers := h.Peerstore().Peers()
	var peerInfos []PeerInfo

	for _, p := range peers {
		if p == h.ID() {
			continue
		}

		addrs := h.Peerstore().Addrs(p)
		addrStrings := make([]string, len(addrs))
		for i, addr := range addrs {
			addrStrings[i] = addr.String()
		}

		latencyVal, err := h.Peerstore().Get(p, "latency")
		var latency int64
		if err == nil {
			if lat, ok := latencyVal.(int64); ok {
				latency = lat
			}
		}

		peerInfos = append(peerInfos, PeerInfo{
			ID:      p.String(),
			Addrs:   addrStrings,
			Latency: latency,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(peerInfos)
}

func connectHandler(w http.ResponseWriter, r *http.Request, h host.Host) {
	var connectReq ConnectRequest
	if r.Body != nil && r.Body != http.NoBody {
		if err := json.NewDecoder(r.Body).Decode(&connectReq); err != nil {
			connectReq.Hops = 1
		}
	} else {
		connectReq.Hops = 1
	}

	if connectReq.Hops <= 0 {
		connectReq.Hops = 1
	}

	log.Printf("[API] Received /api/connect request for %d hop(s)", connectReq.Hops)

	peers := h.Peerstore().Peers()
	var availablePeers []peer.ID

	for _, p := range peers {
		if p == h.ID() {
			continue
		}
		latencyVal, err := h.Peerstore().Get(p, "latency")
		if err != nil {
			continue
		}
		latency, ok := latencyVal.(int64)
		if !ok || latency >= 9999 {
			continue
		}
		availablePeers = append(availablePeers, p)
	}

	if len(availablePeers) < connectReq.Hops {
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("Not enough available peers for %d hops.", connectReq.Hops))
		return
	}

	sort.Slice(availablePeers, func(i, j int) bool {
		latI, _ := h.Peerstore().Get(availablePeers[i], "latency")
		latJ, _ := h.Peerstore().Get(availablePeers[j], "latency")
		return latI.(int64) < latJ.(int64)
	})

	path := availablePeers[:connectReq.Hops]
	wardenPeer := path[0]

	log.Printf("[API] Selected path for %d hops: %v", connectReq.Hops, path)
	log.Printf("[API] Attempting to negotiate tunnel with entry node %s", wardenPeer)

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	seekerPrivKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate private key.")
		return
	}

	var hopIDs []string
	for _, p := range path[1:] {
		hopIDs = append(hopIDs, p.String())
	}

	req := VPNRequest{
		SeekerPublicKey: seekerPrivKey.PublicKey().String(),
		Hops:            hopIDs,
	}

	stream, err := h.NewStream(ctx, wardenPeer, ProtocolStream)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to open stream to peer: %v", err))
		return
	}

	if err := json.NewEncoder(stream).Encode(req); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to send request: %v", err))
		return
	}

	var resp VPNResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get response: %v", err))
		return
	}

	log.Printf("✅ [API] Successfully negotiated multi-hop tunnel. Exit node public key received.")

	log.Printf("[API] Applying configuration to local interface '%s'...", WireGuardInterface)
	if err := configureSeekerInterface(seekerPrivKey, resp.WardenPublicKey, stream.Conn()); err != nil {
		log.Printf("[API] Error configuring WireGuard interface: %v", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to configure WireGuard interface: %v. Try running with sudo?", err))
		return
	}

	log.Printf("✅ [API] Successfully configured WireGuard interface '%s'!", WireGuardInterface)

	w.WriteHeader(http.StatusOK)
	var pathStrings []string
	for _, p := range path {
		pathStrings = append(pathStrings, p.String())
	}

	_ = json.NewEncoder(w).Encode(APIResponse{
		Status:          "success",
		Message:         fmt.Sprintf("WireGuard interface '%s' configured.", WireGuardInterface),
		WardenPeerID:    wardenPeer.String(),
		SeekerPublicKey: seekerPrivKey.PublicKey().String(),
		WardenPublicKey: resp.WardenPublicKey,
		Path:            pathStrings,
	})
}

func disconnectHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("[API] Received /api/disconnect request")

	if err := exec.Command("ip", "link", "del", WireGuardInterface).Run(); err != nil {
		log.Printf("Failed to delete WireGuard interface (it may not exist): %v", err)
	}

	log.Printf("✅ Successfully deleted WireGuard interface '%s'", WireGuardInterface)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Status:  "success",
		Message: "WireGuard interface deleted.",
	})
}

func configureSeekerInterface(privKey wgtypes.Key, wardenPubKeyStr string, conn network.Conn) error {
	if err := exec.Command("ip", "link", "del", WireGuardInterface).Run(); err == nil {
		log.Printf("Removed existing WireGuard interface '%s'", WireGuardInterface)
	}

	if err := exec.Command("ip", "link", "add", WireGuardInterface, "type", "wireguard").Run(); err != nil {
		return fmt.Errorf("failed to create wireguard interface: %w", err)
	}
	log.Printf("Created WireGuard interface '%s'", WireGuardInterface)

	wgClient, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("failed to open wgctrl client: %w", err)
	}
	defer wgClient.Close()

	wardenPubKey, err := wgtypes.ParseKey(wardenPubKeyStr)
	if err != nil {
		return fmt.Errorf("failed to parse warden public key: %w", err)
	}

	addr, _ := multiaddr.NewMultiaddr(strings.Split(conn.RemoteMultiaddr().String(), "/quic-v1")[0])
	remoteAddr, err := manet.ToNetAddr(addr)
	if err != nil {
		return fmt.Errorf("failed to parse remote multiaddr: %w", err)
	}

	udpAddr := remoteAddr.(*net.UDPAddr)

	peer := wgtypes.PeerConfig{
		PublicKey: wardenPubKey,
		AllowedIPs: []net.IPNet{
			{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(0, 32)},
		},
		Endpoint: udpAddr,
	}

	cfg := wgtypes.Config{
		PrivateKey:   &privKey,
		ReplacePeers: true,
		Peers:        []wgtypes.PeerConfig{peer},
	}

	log.Printf("Attempting to configure device '%s' with peer %s", WireGuardInterface, wardenPubKey.String())
	if err := wgClient.ConfigureDevice(WireGuardInterface, cfg); err != nil {
		return fmt.Errorf("failed to configure device: %w", err)
	}

	if err := exec.Command("ip", "link", "set", "up", WireGuardInterface).Run(); err != nil {
		return fmt.Errorf("failed to bring up interface: %w", err)
	}
	log.Printf("Brought up interface '%s'", WireGuardInterface)

	dnsServers := []string{"1.1.1.1", "1.0.0.1"}
	log.Printf("Configuring DNS for interface '%s' with servers: %v", WireGuardInterface, dnsServers)
	if err := exec.Command("resolvectl", "dns", WireGuardInterface, strings.Join(dnsServers, " ")).Run(); err != nil {
		log.Printf("Could not set DNS via resolvectl (maybe not installed or not a systemd-resolved system): %v", err)
	} else {
		if err := exec.Command("resolvectl", "domain", WireGuardInterface, "~.").Run(); err != nil {
			log.Printf("Could not set default DNS domain via resolvectl: %v", err)
		} else {
			log.Println("✅ DNS configured successfully")
		}
	}

	return nil
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(APIResponse{Status: "error", Message: message})
}

// StartNode is the exported function to start the Arkham node
func StartNode(peerOnly bool) {
	h, err := libp2p.New(libp2p.EnableRelay(), libp2p.EnableHolePunching())
	if err != nil {
		log.Fatalf("Failed to create libp2p host: %v", err)
	}

	log.Printf("Arkham P2P Node Initialized: %s", h.ID().String())
	warden := &Warden{host: h}
	h.SetStreamHandler(ProtocolStream, warden.streamHandler)
	h.SetStreamHandler(ProtocolPing, pingHandler)

	go setupDiscovery(h)

	if !peerOnly {
		log.Println("Starting API server on :8080...")
		apiHandler := func(handler http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
				handler(w, r)
			}
		}

		http.HandleFunc("/api/peers", apiHandler(func(w http.ResponseWriter, r *http.Request) {
			peersHandler(w, r, h)
		}))
		http.HandleFunc("/api/connect", apiHandler(func(w http.ResponseWriter, r *http.Request) {
			connectHandler(w, r, h)
		}))
		http.HandleFunc("/api/disconnect", apiHandler(disconnectHandler))

		go func() {
			if err := http.ListenAndServe(":8080", nil); err != nil {
				log.Printf("API server error: %v", err)
			}
		}()
	} else {
		log.Println("Running in peer-only mode.")
	}

	log.Println("Node is running. Press Ctrl+C to stop.")
	select {} // Block forever
}

func setupDiscovery(h host.Host) {
	ctx := context.Background()

	mdnsService := mdns.NewMdnsService(h, ProtocolMDNS, &discoveryNotifee{h: h})
	if err := mdnsService.Start(); err != nil {
		log.Printf("mDNS start error: %v", err)
	}

	kdht, err := kaddht.New(ctx, h)
	if err != nil {
		log.Printf("DHT create error: %v", err)
		return
	}
	if err = kdht.Bootstrap(ctx); err != nil {
		log.Printf("DHT bootstrap error: %v", err)
		return
	}

	routingDiscovery := routing.NewRoutingDiscovery(kdht)
	util.Advertise(ctx, routingDiscovery, ProtocolDHT)

	go func() {
		for {
			peerChan, _ := routingDiscovery.FindPeers(ctx, ProtocolDHT)
			for p := range peerChan {
				if p.ID != h.ID() {
					if len(h.Peerstore().Addrs(p.ID)) == 0 {
						log.Printf("Found peer via DHT: %s", p.ID.String())
						h.Peerstore().AddAddrs(p.ID, p.Addrs, time.Hour)
						go measureLatency(context.Background(), h, p.ID)
					}
				}
			}
			time.Sleep(1 * time.Minute)
		}
	}()

	log.Println("Discovery services running.")
}

type discoveryNotifee struct {
	h host.Host
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.h.ID() {
		return
	}
	log.Printf("Found peer via mDNS: %s", pi.ID.String())
	n.h.Peerstore().AddAddrs(pi.ID, pi.Addrs, time.Hour)

	go measureLatency(context.Background(), n.h, pi.ID)
}
