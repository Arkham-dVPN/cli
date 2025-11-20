package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gagliardetto/solana-go"
)

const (
	configDir  = "config"
	walletFile = "wallet.json"
)

// WalletStorage handles reading from and writing to the wallet file.
type WalletStorage struct {
	filePath string
}

// NewWalletStorage initializes a new WalletStorage.
// It ensures the config directory exists.
func NewWalletStorage() (*WalletStorage, error) {
	// Get the executable path to create the config dir relative to it.
	// This makes the storage location predictable.
	err := os.MkdirAll(configDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &WalletStorage{
		filePath: filepath.Join(configDir, walletFile),
	}, nil
}

// readData reads the entire wallet file and unmarshals it.
func (ws *WalletStorage) readData() (*WalletData, error) {
	data := &WalletData{
		Wallets: make(map[string]solana.PrivateKey),
	}

	file, err := os.ReadFile(ws.filePath)
	if err != nil {
		// If the file doesn't exist, that's okay. We'll create it on the first save.
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, fmt.Errorf("failed to read wallet file: %w", err)
	}

	// If the file is empty, also return a new data object.
	if len(file) == 0 {
		return data, nil
	}

	err = json.Unmarshal(file, data)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal wallet data: %w", err)
	}

	// Ensure the map is not nil if the file contained `{"wallets": null}`
	if data.Wallets == nil {
		data.Wallets = make(map[string]solana.PrivateKey)
	}

	return data, nil
}

// writeData marshals and writes the entire wallet data to the file.
func (ws *WalletStorage) writeData(data *WalletData) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal wallet data: %w", err)
	}

	err = os.WriteFile(ws.filePath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write wallet file: %w", err)
	}
	return nil
}

// SaveWallet saves a private key under a given name.
func (ws *WalletStorage) SaveWallet(name string, privateKey solana.PrivateKey) error {
	data, err := ws.readData()
	if err != nil {
		return err
	}

	data.Wallets[name] = privateKey
	return ws.writeData(data)
}

// GetWallet retrieves a private key by its name.
func (ws *WalletStorage) GetWallet(name string) (solana.PrivateKey, error) {
	data, err := ws.readData()
	if err != nil {
		return nil, err
	}

	privateKey, ok := data.Wallets[name]
	if !ok {
		return nil, fmt.Errorf("wallet '%s' not found", name)
	}

	if len(privateKey) != 64 {
		return nil, fmt.Errorf("invalid private key size for wallet '%s', expected 64, got %d", name, len(privateKey))
	}

	return privateKey, nil
}

// GetAllWalletNames returns a slice of all wallet names.
func (ws *WalletStorage) GetAllWalletNames() ([]string, error) {
	data, err := ws.readData()
	if err != nil {
		return nil, err
	}

	var names []string
	for name := range data.Wallets {
		names = append(names, name)
	}
	return names, nil
}