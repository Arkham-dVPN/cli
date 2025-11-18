package storage

// Wallet represents the wallet data stored in the JSON file.
// We use a byte slice for the private key as it's the raw format
// for database storage and cryptographic operations.
type Wallet struct {
	ID         int    // A fixed ID (e.g., 1) to ensure only one wallet is stored.
	PrivateKey []byte // Stored as bytes (decoded from base64 string in JSON).
}
