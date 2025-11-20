package storage

import "github.com/gagliardetto/solana-go"

// WalletData holds all the wallets managed by the CLI.
// The key of the map is the wallet's name (e.g., "warden", "seeker").
type WalletData struct {
	Wallets map[string]solana.PrivateKey `json:"wallets"`
}