package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

var (
	rpcEndpoint = "https://api.devnet.solana.com"
	endpointInitialized = false
)

// GetRpcEndpoint loads environment variables and returns the best available RPC endpoint.
func GetRpcEndpoint() string {
	if !endpointInitialized {
		if err := godotenv.Load(); err != nil {
			log.Println("Info: .env file not found, using default public RPC endpoint.")
		}

		if heliusApiKey := os.Getenv("HELIUS_API_KEY"); heliusApiKey != "" {
			rpcEndpoint = fmt.Sprintf("https://devnet.helius-rpc.com/?api-key=%s", heliusApiKey)
			log.Println("Info: Using Helius RPC endpoint.")
		}
		endpointInitialized = true
	}
	return rpcEndpoint
}
