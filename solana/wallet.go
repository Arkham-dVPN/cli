package arkham_protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gagliardetto/solana-go"
)

const (
	defaultConfigDirName = ".config"
	arkhamConfigDirName  = "arkham"
	walletFileName       = "wallet.json"
)

// Wallet holds the Solana keypair for the CLI.
type Wallet struct {
	PrivateKey solana.PrivateKey
}

// PublicKey returns the public key of the wallet.
func (w *Wallet) PublicKey() solana.PublicKey {
	return w.PrivateKey.PublicKey()
}

// LoadOrCreateWallet loads a Solana wallet from the default path,
// or creates a new one if it doesn't exist.
func LoadOrCreateWallet() (*Wallet, error) {
	walletPath, err := getWalletPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet path: %w", err)
	}

	// Check if wallet file exists.
	if _, err := os.Stat(walletPath); os.IsNotExist(err) {
		fmt.Println("No existing wallet found. Creating a new one...")
		return createNewWallet(walletPath)
	} else if err != nil {
		return nil, fmt.Errorf("failed to check for wallet file: %w", err)
	}

	fmt.Println("Loading existing wallet from:", walletPath)
	return loadWalletFromFile(walletPath)
}

// createNewWallet generates a new private key and saves it to the specified path.
func createNewWallet(path string) (*Wallet, error) {
	privateKey := solana.NewWallet().PrivateKey
	wallet := &Wallet{PrivateKey: privateKey}

	if err := saveWalletToFile(wallet, path); err != nil {
		return nil, fmt.Errorf("failed to save new wallet: %w", err)
	}

	fmt.Println("✅ New wallet created and saved successfully.")
	fmt.Println("   Address:", wallet.PublicKey().String())
	return wallet, nil
}

// loadWalletFromFile loads a private key from a file.
func loadWalletFromFile(path string) (*Wallet, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read wallet file: %w", err)
	}

	var privateKeyBytes []byte
	if err := json.Unmarshal(bytes, &privateKeyBytes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal wallet file: %w", err)
	}

	if len(privateKeyBytes) != solana.PrivateKeyLength {
		return nil, fmt.Errorf("invalid private key length: expected %d, got %d", solana.PrivateKeyLength, len(privateKeyBytes))
	}

	var privateKey solana.PrivateKey
	copy(privateKey[:], privateKeyBytes)

	return &Wallet{PrivateKey: privateKey}, nil
}

// saveWalletToFile saves the wallet's private key to a file.
func saveWalletToFile(wallet *Wallet, path string) error {
	// Ensure the directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create wallet directory: %w", err)
	}

	// The private key is a slice of 64 bytes.
	bytes, err := json.Marshal(wallet.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := os.WriteFile(path, bytes, 0600); err != nil {
		return fmt.Errorf("failed to write wallet file: %w", err)
	}

	return nil
}

// getWalletPath returns the default absolute path for the wallet file.
// e.g., /home/user/.config/arkham/wallet.json
func getWalletPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(homeDir, defaultConfigDirName, arkhamConfigDirName, walletFileName), nil
}
