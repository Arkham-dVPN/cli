package arkham_protocol

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

var AssociatedTokenProgramID = solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")

// Client is a client for the Arkham Protocol.
type Client struct {
	RpcClient *rpc.Client
	Wallet    *Wallet
	ProgramID solana.PublicKey
}

// NewClient creates a new Client for the Arkham Protocol.
func NewClient(rpcEndpoint string) (*Client, error) {
	// Load or create the integrated wallet.
	wallet, err := LoadOrCreateWallet()
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	// Create a new RPC client.
	rpcClient := rpc.New(rpcEndpoint)

	return &Client{
		RpcClient: rpcClient,
		Wallet:    wallet,
	}, nil
}

// GetProtocolConfigPDA returns the Program Derived Address for the protocol config account.
func (c *Client) GetProtocolConfigPDA() (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("protocol_config"),
		},
		c.ProgramID,
	)
}

// FetchProtocolConfig fetches the protocol configuration from the blockchain.
func (c *Client) FetchProtocolConfig() (*ProtocolConfig, error) {
	protocolConfigPDA, _, err := c.GetProtocolConfigPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get protocol config PDA: %w", err)
	}

	resp, err := c.RpcClient.GetAccountInfo(context.Background(), protocolConfigPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get protocol config account info: %w", err)
	}
	if resp.Value == nil {
		return nil, fmt.Errorf("protocol config account not found")
	}

	return ParseAccount_ProtocolConfig(resp.Value.Data.GetBinary())
}

// Devnet Addresses:
var (
	DevnetPythSolUsdPriceFeed  = solana.MustPublicKeyFromBase58("7UVimffxr9ow1uXYxsr4LHAcV58mLzhmwaeKvJ1pjLiE")
	DevnetPythUsdtUsdPriceFeed = solana.MustPublicKeyFromBase58("HT2PLQBcG5EiCcNSaMHAjSgd9F98ecpATbk4Sk5oYuM")
	DevnetUsdcMint             = solana.MustPublicKeyFromBase58("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
)

// InitializeWarden sends a transaction to the blockchain to initialize a new warden.
func (c *Client) InitializeWarden(
	stakeToken StakeToken,
	stakeAmount uint64,
	peerId string,
	regionCode uint8,
	ipHash [32]uint8,
) (*solana.Signature, error) {

	// Get PDAs:
	wardenPDA, _, err := c.GetWardenPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get warden PDA: %w", err)
	}
	protocolConfigPDA, _, err := c.GetProtocolConfigPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get protocol config PDA: %w", err)
	}
	solVaultPDA, _, err := c.GetSolVaultPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get sol vault PDA: %w", err)
	}

	// Get ATAs for vaults:
	usdcVaultATA, _, err := c.GetUsdcVaultATA(solVaultPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get usdc vault ATA: %w", err)
	}
	usdtVaultATA, _, err := c.GetUsdtVaultATA(solVaultPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get usdt vault ATA: %w", err)
	}

	// Get the source account for the stake.
	// This depends on the token. For SOL, it's the user's wallet.
	// For SPL tokens, it's the user's Associated Token Account.
	var stakeFromAccount solana.PublicKey
	if stakeToken == StakeToken_Sol {
		stakeFromAccount = c.Wallet.PublicKey()
	} else {
		// For SPL tokens, find the user's ATA for that mint.
		mint := DevnetUsdcMint
		if stakeToken == StakeToken_Usdt {
			// NOTE: Using USDC mint as a placeholder for USDT as there is no official one on devnet.
			mint = DevnetUsdcMint
		}
		stakeFromAccount, _, err = solana.FindAssociatedTokenAddress(
			c.Wallet.PublicKey(),
			mint,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to find stake_from ATA: %w", err)
		}
	}

	// Build the instruction.
	instruction, err := NewInitializeWardenInstruction(
		stakeToken,
		stakeAmount,
		peerId,
		regionCode,
		ipHash,
		wardenPDA,
		c.Wallet.PublicKey(),
		protocolConfigPDA,
		stakeFromAccount,
		solVaultPDA,
		usdcVaultATA,
		usdtVaultATA,
		DevnetUsdcMint,
		DevnetUsdcMint, // Placeholder for USDT mint
		DevnetPythSolUsdPriceFeed,
		DevnetPythUsdtUsdPriceFeed,
		solana.SystemProgramID,
		solana.TokenProgramID,
		AssociatedTokenProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create instruction: %w", err)
	}

	// Get recent blockhash
	recentBlockhash, err := c.RpcClient.GetRecentBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	// Create a new transaction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		recentBlockhash.Value.Blockhash,
		solana.TransactionPayer(c.Wallet.PublicKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Sign the transaction
	_, err = tx.Sign(
		func(key solana.PublicKey) *solana.PrivateKey {
			if c.Wallet.PublicKey().Equals(key) {
				return &c.Wallet.PrivateKey
			}
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send the transaction
	sig, err := c.RpcClient.SendTransaction(context.Background(), tx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return &sig, nil
}

// GetWardenPDA returns the PDA for the current user's warden account.
func (c *Client) GetWardenPDA() (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("warden"),
			c.Wallet.PublicKey().Bytes(),
		},
		c.ProgramID,
	)
}

// GetSolVaultPDA returns the PDA for the protocol's SOL vault.
func (c *Client) GetSolVaultPDA() (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("sol_vault"),
		},
		c.ProgramID,
	)
}

// GetUsdcVaultATA returns the ATA for the protocol's USDC vault.
func (c *Client) GetUsdcVaultATA(solVaultPDA solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindAssociatedTokenAddress(
		solVaultPDA,
		DevnetUsdcMint,
	)
}

// GetUsdtVaultATA returns the ATA for the protocol's USDT vault.
func (c *Client) GetUsdtVaultATA(solVaultPDA solana.PublicKey) (solana.PublicKey, uint8, error) {
	// NOTE: Using USDC mint as a placeholder for USDT.
	return solana.FindAssociatedTokenAddress(
		solVaultPDA,
		DevnetUsdcMint,
	)
}
