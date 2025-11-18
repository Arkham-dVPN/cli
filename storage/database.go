package storage

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gagliardetto/solana-go"
)

const (
	walletFileName = "wallet.json"
	configDirName  = "config"
)

// WalletData represents the wallet data stored in the JSON file.
type WalletData struct {
	ID         int    `json:"id"`
	PrivateKey string `json:"private_key"` // Stored as base64 encoded string
}

// JSONDB provides a connection to the JSON-based storage.
type JSONDB struct {
	path string
}

// Connect opens and initializes the JSON-based storage.
func Connect() (*JSONDB, error) {
	dbPath, err := getDBPath()
	if err != nil {
		return nil, fmt.Errorf("could not get db path: %w", err)
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("could not create db directory: %w", err)
	}

	db := &JSONDB{path: dbPath}

	// Initialize with empty file if it doesn't exist
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// Create an empty file
		file, err := os.Create(dbPath)
		if err != nil {
			return nil, fmt.Errorf("could not create wallet file: %w", err)
		}
		file.Close()
	}

	return db, nil
}

// GetWallet retrieves the stored wallet from the JSON file.
// It returns an error if no wallet is found.
func (db *JSONDB) GetWallet() (*Wallet, error) {
	data, err := os.ReadFile(db.path)
	if err != nil {
		return nil, fmt.Errorf("could not read wallet file: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("no wallet found")
	}

	var walletData WalletData
	if err := json.Unmarshal(data, &walletData); err != nil {
		return nil, fmt.Errorf("could not parse wallet file: %w", err)
	}

	if walletData.PrivateKey == "" {
		return nil, fmt.Errorf("no wallet found")
	}

	// Decode the base64 private key string back to bytes
	privateKeyBytes, err := base64.StdEncoding.DecodeString(walletData.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("could not decode private key: %w", err)
	}

	if len(privateKeyBytes) != solana.PrivateKeyLength {
		return nil, fmt.Errorf("invalid private key length in wallet file: expected %d, got %d", solana.PrivateKeyLength, len(privateKeyBytes))
	}

	return &Wallet{
		ID:         walletData.ID,
		PrivateKey: privateKeyBytes,
	}, nil
}

// SaveWallet saves a new wallet to the JSON file.
func (db *JSONDB) SaveWallet(privateKey solana.PrivateKey) error {
	// Encode the private key as base64 string for JSON storage
	privateKeyB64 := base64.StdEncoding.EncodeToString(privateKey[:])

	walletData := WalletData{
		ID:         1, // Fixed ID for single wallet
		PrivateKey: privateKeyB64,
	}

	data, err := json.Marshal(walletData)
	if err != nil {
		return fmt.Errorf("could not marshal wallet data: %w", err)
	}

	if err := os.WriteFile(db.path, data, 0644); err != nil {
		return fmt.Errorf("could not write wallet file: %w", err)
	}

	return nil
}

// getDBPath returns the path for the wallet file relative to the current working directory.
func getDBPath() (string, error) {
	// This assumes the CLI is run from the `arkham-cli` directory.
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not get current working directory: %w", err)
	}
	return filepath.Join(cwd, configDirName, walletFileName), nil
}

// Close closes the JSON database connection (for interface compatibility).
// Since this is a JSON file implementation, there's no actual connection to close.
func (db *JSONDB) Close() error {
	return nil
}
