# Arkham CLI

A unified command-line interface for the Arkham decentralized VPN network. This single executable contains everything needed to run an Arkham node - from the interactive UI to the complete P2P networking logic.

## 🏗️ Architecture

The Arkham CLI represents a complete architectural refactor where the backend P2P logic has been converted into a reusable Go library (`arkham-cli/node`) and integrated directly into the CLI application. This creates a single, professional executable instead of the previous multi-process approach.

### Project Structure

```
arkham-cli/
├── main.go           # Entry point
├── cmd/
│   └── root.go       # Interactive CLI logic with Cobra
├── node/
│   └── node.go       # P2P networking library (formerly the backend)
└── go.mod            # Dependencies
```

## ✨ Features

### Implemented from Whitepaper

- **Multi-Hop Routing**: Chain up to multiple peers for enhanced privacy
- **Intelligent Peer Selection**: Latency-based peer selection for optimal performance
- **Secure DNS**: Automatic DNS configuration to prevent leaks
- **P2P Discovery**: Both mDNS (local) and DHT (global) peer discovery
- **WireGuard Integration**: Secure, high-performance VPN tunneling

### CLI Features

- **Interactive Menu**: Beautiful, user-friendly terminal interface
- **Gateway Node Mode**: Full-featured node with API server for web dashboard
- **Peer-Only Mode**: Lightweight mode that only participates in the network
- **Permission Checking**: Automatic detection of required sudo privileges
- **Quick Actions**: Open web dashboard and GitHub directly from the CLI

## 🚀 Quick Start

### Prerequisites

- Go 1.21 or higher
- Linux system with WireGuard support
- `sudo` access (for Gateway mode)

### Installation

```bash
# Clone the repository
cd arkham-cli

# Install dependencies
go mod download

# Build the executable
go build -o arkham-cli

# Install globally (optional)
sudo mv arkham-cli /usr/local/bin/
```

### Running the CLI

#### Interactive Mode (Recommended)

```bash
# For Gateway Node (requires sudo)
sudo ./arkham-cli

# Or if installed globally
sudo arkham-cli
```

The interactive menu will appear:

```
   █████╗ ██████╗ ██╗  ██╗██╗  ██╗ █████╗ ███╗   ███╗
  ██╔══██╗██╔══██╗██║ ██╔╝██║  ██║██╔══██╗████╗ ████║
  ███████║██████╔╝█████╔╝ ███████║███████║██╔████╔██║
  ██╔══██║██╔══██╗██╔═██╗ ██╔══██║██╔══██║██║╚██╔╝██║
  ██║  ██║██║  ██║██║  ██╗██║  ██║██║  ██║██║ ╚═╝ ██║
  ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝     ╚═╝

Choose an action:
> Start Gateway Node
  Start Peer-Only Node
  View Credits
  Open Web Dashboard
  Open GitHub Repository
  Exit
```

## 🎯 Usage Modes

### Gateway Node

**Requirements**: Must run with `sudo`

This mode starts a full-featured Arkham node that:
- Discovers and connects to other peers
- Runs an API server on port 8080
- Manages WireGuard network interfaces
- Serves as both entry and exit node for other clients
- Provides endpoints for the web dashboard

```bash
sudo arkham-cli
# Then select "Start Gateway Node"
```

The API server exposes:
- `GET /api/peers` - List all discovered peers with latency
- `POST /api/connect` - Establish a VPN tunnel (supports multi-hop)
- `POST /api/disconnect` - Tear down the VPN tunnel

### Peer-Only Node

**Requirements**: No special permissions needed

This mode runs a lightweight node that:
- Participates in peer discovery
- Can serve as intermediate or exit node for others
- Does NOT run the API server
- Does NOT manage local network interfaces

```bash
./arkham-cli
# Then select "Start Peer-Only Node"
```

Perfect for contributing to the network without using it yourself.

## 🔧 Technical Details

### Key Refactoring Changes

1. **Package Structure**: Changed from `package main` to `package node` in `node.go`
2. **Exported Function**: Created `StartNode(peerOnly bool)` as the public API
3. **Direct Integration**: CLI now imports and calls the node library directly
4. **Single Binary**: No more external process spawning or inter-process communication

### P2P Protocols

- **mDNS Protocol**: `arkham-vpn-local` (for local network discovery)
- **DHT Protocol**: `arkham-vpn-global` (for internet-wide discovery)
- **Stream Protocol**: `/arkham/vpn/1.0.0` (for VPN negotiation)
- **Ping Protocol**: `/arkham/ping/1.0.0` (for latency measurement)

### Multi-Hop Flow

1. Client selects N peers based on lowest latency
2. Client connects to first peer (entry node)
3. Entry node forwards request to next hop
4. Each intermediate node forwards the request
5. Exit node generates WireGuard keys and responds
6. Response propagates back through the chain
7. Client configures local WireGuard interface

## 🌐 Web Dashboard

The Arkham CLI works seamlessly with the web dashboard:

```bash
# From the CLI menu, select "Open Web Dashboard"
# Or visit manually:
https://arkham-dvpn.vercel.app/
```

The dashboard connects to your local Gateway Node's API (port 8080) to:
- Display discovered peers with latency metrics
- Visualize the multi-hop tunnel path
- Control connection/disconnection
- Show real-time network status

## 🛠️ Development

### Project Structure Explained

```
arkham-cli/
├── main.go                 # Initializes and executes the Cobra CLI
├── cmd/
│   └── root.go            # Cobra commands and interactive menu
│       ├── runInteractive() - Main menu loop
│       ├── startGatewayNode() - Launches node with API
│       ├── startPeerNode() - Launches peer-only node
│       └── isRoot() - Permission checking
└── node/
    └── node.go            # Complete P2P and VPN logic
        ├── StartNode() - Main entry point (exported)
        ├── Warden{} - Handles incoming VPN requests
        ├── setupDiscovery() - mDNS + DHT initialization
        ├── measureLatency() - Peer latency probing
        ├── connectHandler() - API endpoint for tunneling
        └── configureSeekerInterface() - WireGuard setup
```

### Building from Source

```bash
# Standard build
go build -o arkham-cli

# Optimized release build
go build -ldflags="-s -w" -o arkham-cli

# Cross-compilation example (from macOS/Linux to Linux)
GOOS=linux GOARCH=amd64 go build -o arkham-cli-linux
```

### Testing

```bash
# Start multiple peer-only nodes for testing
./arkham-cli  # Select "Start Peer-Only Node"

# In another terminal, start a gateway node
sudo ./arkham-cli  # Select "Start Gateway Node"

# Check peer discovery in the logs
# Connect via the web dashboard
```

## 📋 API Reference

### GET /api/peers

Returns all discovered peers with connection information.

**Response:**
```json
[
  {
    "id": "12D3KooWABC...",
    "addrs": ["/ip4/192.168.1.100/tcp/12345"],
    "latency": 45
  }
]
```

### POST /api/connect

Establishes a VPN tunnel through the network.

**Request:**
```json
{
  "hops": 3
}
```

**Response:**
```json
{
  "status": "success",
  "message": "WireGuard interface 'wg0' configured.",
  "warden_peer_id": "12D3KooWXYZ...",
  "seeker_public_key": "abc123...",
  "warden_public_key": "def456...",
  "path": ["12D3KooW1...", "12D3KooW2...", "12D3KooW3..."]
}
```

### POST /api/disconnect

Tears down the active VPN tunnel.

**Response:**
```json
{
  "status": "success",
  "message": "WireGuard interface deleted."
}
```

## 🐛 Troubleshooting

### "Permission denied" when starting Gateway Node

**Solution**: Run with `sudo`
```bash
sudo arkham-cli
```

### "Not enough available peers for N hops"

**Solution**: Wait for more peers to be discovered, or reduce the number of hops
- Peers are discovered continuously via mDNS and DHT
- Check logs for "Found peer via..." messages
- Try peer-only nodes on the same network first

### WireGuard interface configuration fails

**Solution**: Ensure WireGuard kernel module is loaded
```bash
# Check if WireGuard is available
sudo modprobe wireguard
lsmod | grep wireguard

# Install WireGuard if needed (Ubuntu/Debian)
sudo apt install wireguard
```

### DNS not working through VPN

**Solution**: Ensure `systemd-resolved` is running
```bash
systemctl status systemd-resolved

# If not running
sudo systemctl start systemd-resolved
```

## 🎓 Architecture Benefits

### Before Refactor
- ❌ Multiple processes (CLI + Backend)
- ❌ Complex inter-process communication
- ❌ Difficult to debug
- ❌ Larger deployment footprint

### After Refactor
- ✅ Single unified executable
- ✅ Direct function calls
- ✅ Clean library architecture
- ✅ Easy to maintain and extend
- ✅ Professional codebase structure

## 📝 Credits

**Project Author**: Skipp  
**X/Twitter**: [@davidnzubee](https://x.com/davidnzubee)  
**GitHub**: [Arkham-dVPN](https://github.com/Arkham-dVPN)

## 📄 License

[Add your license here]

## 🚧 Future Enhancements

- Token-based incentivization system
- Enhanced privacy with traffic obfuscation
- Cross-platform support (Windows, macOS)
- Mobile app integration
- Network analytics dashboard
- Automatic peer reputation system

---

**Built for the hackathon with ❤️**
