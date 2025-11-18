package arkham_protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
	"golang.org/x/crypto/sha3"
)

var AssociatedTokenProgramID = solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
var Ed25519ProgramID = solana.MustPublicKeyFromBase58("Ed25519SigVerify111111111111111111111111111")

// Client is a client for the Arkham Protocol.
type Client struct {
	RpcClient *rpc.Client
	Signer    solana.PrivateKey
}

// NewClient creates a new Client for the Arkham Protocol.
func NewClient(rpcEndpoint string, signer solana.PrivateKey) (*Client, error) {
	// Create a new RPC client.
	rpcClient := rpc.New(rpcEndpoint)

	return &Client{
		RpcClient: rpcClient,
		Signer:    signer,
	}, nil
}

// GetProtocolConfigPDA returns the Program Derived Address for the protocol config account.
func (c *Client) GetProtocolConfigPDA() (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("protocol_config"),
		},
		ProgramID,
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
	DevnetUsdcMint = solana.MustPublicKeyFromBase58("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	// Using USDC mint as a placeholder for USDT as there is no official one on devnet.
	DevnetUsdtMint = solana.MustPublicKeyFromBase58("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
)

// InitializeWarden sends a transaction to the blockchain to initialize a new warden.
func (c *Client) InitializeWarden(
	stakeToken StakeToken,
	stakeAmount uint64,
	peerId string,
	regionCode uint8,
	ipHash [32]uint8,
) (*solana.Signature, error) {

	// 1. Fetch price data from the oracle API
	// -----------------------------------------
	trustedKey := os.Getenv("TRUSTED_CLIENT_KEY")
	if trustedKey == "" {
		return nil, fmt.Errorf("TRUSTED_CLIENT_KEY not set in .env file")
	}

	tokenStr := ""
	switch stakeToken {
	case StakeToken_Sol:
		tokenStr = "solana"
	case StakeToken_Usdc:
		tokenStr = "usd-coin"
	case StakeToken_Usdt:
		tokenStr = "tether"
	default:
		return nil, fmt.Errorf("unsupported stake token")
	}

	// TODO: Make the base URL configurable
	baseURL := "https://arkham-dvpn.vercel.app/api/price"
	params := url.Values{}
	params.Add("token", tokenStr)
	params.Add("trustedClientKey", trustedKey)
	reqURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	resp, err := http.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to call price API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("price API returned non-200 status: %s - %s", resp.Status, string(body))
	}

	var priceResp struct {
		Price     string `json:"price"`
		Timestamp string `json:"timestamp"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&priceResp); err != nil {
		return nil, fmt.Errorf("failed to decode price API response: %w", err)
	}

	price, err := strconv.ParseUint(priceResp.Price, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse price from API: %w", err)
	}
	timestamp, err := strconv.ParseInt(priceResp.Timestamp, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp from API: %w", err)
	}
	signature, err := hex.DecodeString(priceResp.Signature)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature from API: %w", err)
	}
	if len(signature) != 64 {
		return nil, fmt.Errorf("invalid signature length from API: expected 64, got %d", len(signature))
	}
	var finalSignature [64]byte
	copy(finalSignature[:], signature)

	// 2. Recreate the oracle message hash to ensure integrity
	// -------------------------------------------------------
	oracleMsgBuffer := new(bytes.Buffer)
	binary.Write(oracleMsgBuffer, binary.LittleEndian, price)
	binary.Write(oracleMsgBuffer, binary.LittleEndian, timestamp)

	hasher := sha3.NewLegacyKeccak256()
	hasher.Write(oracleMsgBuffer.Bytes())
	messageHash := hasher.Sum(nil)

	// 3. Build the Ed25519 instruction
	// ---------------------------------
	protocolConfig, err := c.FetchProtocolConfig()
	if err != nil {
		return nil, fmt.Errorf("could not fetch protocol config to get oracle authority: %w", err)
	}
	oracleAuthority := protocolConfig.OracleAuthority

	// Manually construct the Ed25519 instruction data payload
	// The header is 16 bytes long, so the signature starts at offset 16.
	sigOffset := uint16(16)
	keyOffset := sigOffset + 64
	msgOffset := keyOffset + 32
	
	ed25519InstrData := []byte{1, 0} // num_signatures, padding
	ed25519InstrData = binary.LittleEndian.AppendUint16(ed25519InstrData, sigOffset)
	ed25519InstrData = binary.LittleEndian.AppendUint16(ed25519InstrData, 0xFFFF) // sig instruction index
	ed25519InstrData = binary.LittleEndian.AppendUint16(ed25519InstrData, keyOffset)
	ed25519InstrData = binary.LittleEndian.AppendUint16(ed25519InstrData, 0xFFFF) // key instruction index
	ed25519InstrData = binary.LittleEndian.AppendUint16(ed25519InstrData, msgOffset)
	ed25519InstrData = binary.LittleEndian.AppendUint16(ed25519InstrData, uint16(len(messageHash)))
	ed25519InstrData = binary.LittleEndian.AppendUint16(ed25519InstrData, 0xFFFF) // msg instruction index
	
	ed25519InstrData = append(ed25519InstrData, signature...)
	ed25519InstrData = append(ed25519InstrData, oracleAuthority[:]...)
	ed25519InstrData = append(ed25519InstrData, messageHash...)

	ed25519Instruction := solana.NewInstruction(
		Ed25519ProgramID,
		[]*solana.AccountMeta{},
		ed25519InstrData,
	)

	// 4. Build the InitializeWarden instruction
	// -----------------------------------------
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
	usdcVaultATA, _, err := c.GetUsdcVaultATA(solVaultPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get usdc vault ATA: %w", err)
	}
	usdtVaultATA, _, err := c.GetUsdtVaultATA(solVaultPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get usdt vault ATA: %w", err)
	}

	var stakeFromAccount solana.PublicKey
	if stakeToken == StakeToken_Sol {
		stakeFromAccount = c.Signer.PublicKey()
	} else {
		mint := DevnetUsdcMint
		if stakeToken == StakeToken_Usdt {
			mint = DevnetUsdtMint
		}
		stakeFromAccount, _, err = solana.FindAssociatedTokenAddress(c.Signer.PublicKey(), mint)
		if err != nil {
			return nil, fmt.Errorf("failed to find stake_from ATA: %w", err)
		}
	}

	initWardenInstruction, err := NewInitializeWardenInstruction(
		stakeToken,
		stakeAmount,
		peerId,
		regionCode,
		ipHash,
		price,
		timestamp,
		finalSignature,
		wardenPDA,
		c.Signer.PublicKey(),
		protocolConfigPDA,
		solana.SysVarInstructionsPubkey,
		stakeFromAccount,
		solVaultPDA,
		usdcVaultATA,
		usdtVaultATA,
		DevnetUsdcMint,
		DevnetUsdtMint,
		solana.SystemProgramID,
		solana.TokenProgramID,
		AssociatedTokenProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create InitializeWarden instruction: %w", err)
	}

	// 5. Build and send the transaction
	// ---------------------------------
	latestBlockhash, err := c.RpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			ed25519Instruction,
			initWardenInstruction,
		},
		latestBlockhash.Value.Blockhash,
		solana.TransactionPayer(c.Signer.PublicKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	_, err = tx.Sign(
		func(key solana.PublicKey) *solana.PrivateKey {
			if c.Signer.PublicKey().Equals(key) {
				return &c.Signer
			}
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	sig, err := c.RpcClient.SendTransaction(context.Background(), tx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return &sig, nil
}

// SendSol sends a specified amount of SOL to a recipient.
func (c *Client) SendSol(recipient solana.PublicKey, amountLamports uint64) (*solana.Signature, error) {
	latestBlockhash, err := c.RpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	instruction := system.NewTransferInstruction(
		amountLamports,
		c.Signer.PublicKey(),
		recipient,
	).Build()

	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		latestBlockhash.Value.Blockhash,
		solana.TransactionPayer(c.Signer.PublicKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	_, err = tx.Sign(
		func(key solana.PublicKey) *solana.PrivateKey {
			if c.Signer.PublicKey().Equals(key) {
				return &c.Signer
			}
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	sig, err := c.RpcClient.SendTransaction(context.Background(), tx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return &sig, nil
}

// GetBalance retrieves the SOL balance for a given public key.
func (c *Client) GetBalance(publicKey solana.PublicKey) (uint64, error) {
	balance, err := c.RpcClient.GetBalance(
		context.Background(),
		publicKey,
		rpc.CommitmentFinalized,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get balance: %w", err)
	}
	return balance.Value, nil
}

// GetWardenPDA returns the PDA for the current user's warden account.
func (c *Client) GetWardenPDA() (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("warden"),
			c.Signer.PublicKey().Bytes(),
		},
		ProgramID,
	)
}

// GetSolVaultPDA returns the PDA for the protocol's SOL vault.
func (c *Client) GetSolVaultPDA() (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("sol_vault"),
		},
		ProgramID,
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
	return solana.FindAssociatedTokenAddress(
		solVaultPDA,
		DevnetUsdtMint,
	)
}

// IsWardenRegistered checks if the client's signer already has a Warden account on-chain.
func (c *Client) IsWardenRegistered() (bool, error) {
	wardenPDA, _, err := c.GetWardenPDA()
	if err != nil {
		return false, fmt.Errorf("failed to get warden PDA for check: %w", err)
	}

	resp, err := c.RpcClient.GetAccountInfo(context.Background(), wardenPDA)
	if err != nil {
		// Consider RPC errors as a reason to halt, rather than assuming not registered.
		return false, fmt.Errorf("failed to get warden account info: %w", err)
	}

	// If the account exists and has data, the user is registered.
	return resp.Value != nil, nil
}
