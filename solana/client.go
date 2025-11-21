package arkham_protocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

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

// NewClient creates a new Client for the Arkham Protocol with a specific signer.
func NewClient(rpcEndpoint string, signer solana.PrivateKey) (*Client, error) {
	// Create a new RPC client.
	rpcClient := rpc.New(rpcEndpoint)

	return &Client{
		RpcClient: rpcClient,
		Signer:    signer,
	}, nil
}

// NewReadOnlyClient creates a new client for read-only operations that don't require a signer.
// It uses a dummy keypair internally.
func NewReadOnlyClient(rpcEndpoint string) (*Client, error) {
	// Create a new RPC client.
	rpcClient := rpc.New(rpcEndpoint)

	// Create a dummy wallet for read-only operations.
	dummyWallet := solana.NewWallet()

	return &Client{
		RpcClient: rpcClient,
		Signer:    dummyWallet.PrivateKey,
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

	resp, err := c.RpcClient.GetAccountInfoWithOpts(context.Background(), protocolConfigPDA, &rpc.GetAccountInfoOpts{
		Commitment: rpc.CommitmentConfirmed,
	})
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

// SubmitBandwidthProof sends a transaction to the blockchain to submit a bandwidth proof.
func (c *Client) SubmitBandwidthProof(
	mbConsumed uint64,
	seekerPublicKey solana.PublicKey,
	seekerSignature solana.Signature,
	timestamp int64,
) (*solana.Signature, error) {

	// 1. Derive all required PDAs
	// -----------------------------
	wardenPublicKey := c.Signer.PublicKey()
	wardenPDA, _, err := c.GetWardenPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get warden PDA: %w", err)
	}
	seekerPDA, _, err := GetSeekerPDA(seekerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get seeker PDA: %w", err)
	}

	// FIX: Use PDAs (not authorities) for connection PDA
	connectionPDA, _, err := GetConnectionPDA(seekerPDA, wardenPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection PDA: %w", err)
	}
	protocolConfigPDA, _, err := c.GetProtocolConfigPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get protocol config PDA: %w", err)
	}

	// 2. Construct the message that was signed
	// -----------------------------------------
	msgBuffer := new(bytes.Buffer)
	msgBuffer.Write(connectionPDA.Bytes())
	binary.Write(msgBuffer, binary.LittleEndian, mbConsumed)
	binary.Write(msgBuffer, binary.LittleEndian, timestamp)

	hasher := sha3.NewLegacyKeccak256()
	hasher.Write(msgBuffer.Bytes())
	messageHash := hasher.Sum(nil)

	// 3. Generate the Warden's signature
	// -----------------------------------
	wardenSignature, err := c.Signer.Sign(messageHash)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message as warden: %w", err)
	}

	// 4. Build the Ed25519 instructions
	// -----------------------------------
	// Ed25519 instruction data layout:
	// [num_signatures: u8, padding: u8,
	//  sig_offset: u16, sig_index: u16,
	//  pk_offset: u16, pk_index: u16,
	//  msg_offset: u16, msg_size: u16, msg_index: u16,
	//  signature: 64 bytes, public_key: 32 bytes, message: variable bytes]

	sigOffset := uint16(16) // Header is 16 bytes
	pkOffset := sigOffset + 64
	msgOffset := pkOffset + 32

	// FIX: Create SEEKER instruction FIRST (to match Rust expectation at index 0)
	seekerSigIxData := new(bytes.Buffer)
	seekerSigIxData.WriteByte(1) // num_signatures
	seekerSigIxData.WriteByte(0) // padding
	binary.Write(seekerSigIxData, binary.LittleEndian, sigOffset)
	binary.Write(seekerSigIxData, binary.LittleEndian, uint16(0xFFFF))
	binary.Write(seekerSigIxData, binary.LittleEndian, pkOffset)
	binary.Write(seekerSigIxData, binary.LittleEndian, uint16(0xFFFF))
	binary.Write(seekerSigIxData, binary.LittleEndian, msgOffset)
	binary.Write(seekerSigIxData, binary.LittleEndian, uint16(len(messageHash)))
	binary.Write(seekerSigIxData, binary.LittleEndian, uint16(0xFFFF))
	seekerSigIxData.Write(seekerSignature[:]) // Signature bytes
	seekerSigIxData.Write(seekerPublicKey[:]) // Public key bytes
	seekerSigIxData.Write(messageHash)        // Message hash

	seekerSigInstruction := solana.NewInstruction(
		Ed25519ProgramID,
		[]*solana.AccountMeta{},
		seekerSigIxData.Bytes(),
	)

	// Create WARDEN instruction SECOND (to match Rust expectation at index 1)
	wardenSigIxData := new(bytes.Buffer)
	wardenSigIxData.WriteByte(1) // num_signatures
	wardenSigIxData.WriteByte(0) // padding
	binary.Write(wardenSigIxData, binary.LittleEndian, sigOffset)
	binary.Write(wardenSigIxData, binary.LittleEndian, uint16(0xFFFF))
	binary.Write(wardenSigIxData, binary.LittleEndian, pkOffset)
	binary.Write(wardenSigIxData, binary.LittleEndian, uint16(0xFFFF))
	binary.Write(wardenSigIxData, binary.LittleEndian, msgOffset)
	binary.Write(wardenSigIxData, binary.LittleEndian, uint16(len(messageHash)))
	binary.Write(wardenSigIxData, binary.LittleEndian, uint16(0xFFFF))
	wardenSigIxData.Write(wardenSignature[:]) // Signature bytes
	wardenSigIxData.Write(wardenPublicKey[:]) // Public key bytes
	wardenSigIxData.Write(messageHash)        // Message hash

	wardenSigInstruction := solana.NewInstruction(
		Ed25519ProgramID,
		[]*solana.AccountMeta{},
		wardenSigIxData.Bytes(),
	)

	// 5. Build the main SubmitBandwidthProof instruction
	// --------------------------------------------------
	submitProofInstruction, err := NewSubmitBandwidthProofInstruction(
		mbConsumed,
		timestamp,
		seekerSignature,
		wardenSignature,
		connectionPDA,
		wardenPDA,
		seekerPDA,
		protocolConfigPDA,
		solana.SysVarInstructionsPubkey,
		c.Signer.PublicKey(), // Submitter is the warden
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SubmitBandwidthProof instruction: %w", err)
	}

	// 6. Build and send the transaction
	// ---------------------------------
	// FIX: Order is now SEEKER, WARDEN, SUBMIT (matches Rust expectation)
	latestBlockhash, err := c.RpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			seekerSigInstruction,   // Index 0 - Seeker
			wardenSigInstruction,   // Index 1 - Warden
			submitProofInstruction, // Index 2 - Main instruction
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

// GenerateBandwidthProofSignature creates a signature for a bandwidth proof.
func (c *Client) GenerateBandwidthProofSignature(
	wardenAuthority solana.PublicKey,
	mbConsumed uint64,
	timestamp int64,
) (solana.Signature, error) {
	seekerAuthority := c.Signer.PublicKey()

	// FIX: Get the actual PDAs first
	seekerPDA, _, err := GetSeekerPDA(seekerAuthority)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("failed to get seeker PDA: %w", err)
	}
	wardenPDA, _, err := GetWardenPDAForAuthority(wardenAuthority)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("failed to get warden PDA: %w", err)
	}

	// FIX: Use PDAs (not authorities) for connection PDA
	connectionPDA, _, err := GetConnectionPDA(seekerPDA, wardenPDA)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("failed to get connection PDA: %w", err)
	}

	// Construct the exact same message as the smart contract expects
	msgBuffer := new(bytes.Buffer)
	msgBuffer.Write(connectionPDA.Bytes())
	binary.Write(msgBuffer, binary.LittleEndian, mbConsumed)
	binary.Write(msgBuffer, binary.LittleEndian, timestamp)

	hasher := sha3.NewLegacyKeccak256()
	hasher.Write(msgBuffer.Bytes())
	messageHash := hasher.Sum(nil)

	seekerSignature, err := c.Signer.Sign(messageHash)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("failed to sign message as seeker: %w", err)
	}

	return seekerSignature, nil
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

// GetTokenBalance retrieves the balance for a specific token mint for a given public key.
func (c *Client) GetTokenBalance(owner solana.PublicKey, mint solana.PublicKey) (uint64, error) {
	// Find the associated token address.
	ata, _, err := solana.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		return 0, fmt.Errorf("failed to find associated token address: %w", err)
	}

	// Get the balance of the ATA.
	balance, err := c.RpcClient.GetTokenAccountBalance(
		context.Background(),
		ata,
		rpc.CommitmentFinalized,
	)
	if err != nil {
		// If the account is not found, it just means the balance is 0.
		// Check for the specific RPC error text.
		if err.Error() == "not found" || strings.Contains(err.Error(), "could not find account") {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get token account balance for ATA %s: %w", ata.String(), err)
	}

	if balance.Value == nil {
		return 0, nil // Account exists but has no value object, treat as 0.
	}

	// The balance is returned as a string, parse it to uint64.
	amount, err := strconv.ParseUint(balance.Value.Amount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse token amount string: %w", err)
	}

	return amount, nil
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

// GetSeekerPDA returns the PDA for a given seeker's authority.
func GetSeekerPDA(seekerAuthority solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("seeker"),
			seekerAuthority.Bytes(),
		},
		ProgramID,
	)
}

// GetConnectionPDA returns the PDA for a connection.
func GetConnectionPDA(seekerPDA, wardenPDA solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("connection"),
			seekerPDA.Bytes(),
			wardenPDA.Bytes(),
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

	resp, err := c.RpcClient.GetAccountInfoWithOpts(context.Background(), wardenPDA, &rpc.GetAccountInfoOpts{
		Commitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		// The account not being found is an error from the RPC client's perspective,
		// but for our logic, it's a valid state meaning "not registered".
		// We check if the response value is nil, which is true when the account is not found.
		// Any other error (network, etc.) is a real problem.
		if resp == nil {
			return false, nil
		}
		return false, fmt.Errorf("failed to get warden account info: %w", err)
	}

	// If there was no RPC error and the account value is nil, it also means not found.
	if resp.Value == nil {
		return false, nil
	}

	// If we have the account, the user is registered.
	return true, nil
}

// IsSeekerRegistered checks if a Seeker account exists for the client's public key.
func (c *Client) IsSeekerRegistered() (bool, error) {
	seekerPDA, _, err := GetSeekerPDA(c.Signer.PublicKey())
	if err != nil {
		return false, fmt.Errorf("failed to get seeker PDA: %w", err)
	}

	_, err = c.RpcClient.GetAccountInfo(context.Background(), seekerPDA)
	if err != nil {
		if err == rpc.ErrNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to check for seeker account: %w", err)
	}
	return true, nil
}

// DepositEscrow deposits SOL into the seeker's on-chain escrow account.
func (c *Client) DepositEscrow(amountLamports uint64) (*solana.Signature, error) {
	// The Seeker is the signer for this transaction.
	seekerAuthority := c.Signer.PublicKey()
	seekerPDA, _, err := GetSeekerPDA(seekerAuthority)
	if err != nil {
		return nil, fmt.Errorf("failed to get seeker PDA: %w", err)
	}

	// The new smart contract (with init_if_needed) handles account creation.
	// We just need to call the single instruction.
	depositInstruction, err := NewDepositEscrowInstruction(
		amountLamports,
		false, // usePrivate is false for now
		seekerPDA,
		seekerAuthority,
		solana.SystemProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DepositEscrow instruction: %w", err)
	}

	latestBlockhash, err := c.RpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{depositInstruction},
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

func (c *Client) StartConnection(wardenAuthority solana.PublicKey, estimatedMb uint64) (*solana.Signature, error) {
	seekerAuthority := c.Signer.PublicKey()

	// First get the PDAs for seeker and warden
	seekerPDA, _, err := GetSeekerPDA(seekerAuthority)
	if err != nil {
		return nil, fmt.Errorf("failed to get seeker PDA: %w", err)
	}
	wardenPDA, _, err := GetWardenPDAForAuthority(wardenAuthority)
	if err != nil {
		return nil, fmt.Errorf("failed to get warden PDA: %w", err)
	}

	// FIX: Now derive connection PDA using the actual PDAs
	connectionPDA, _, err := GetConnectionPDA(seekerPDA, wardenPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection PDA: %w", err)
	}

	protocolConfigPDA, _, err := c.GetProtocolConfigPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get protocol config PDA: %w", err)
	}

	instruction, err := NewStartConnectionInstruction(
		estimatedMb,
		connectionPDA,
		seekerPDA,
		wardenPDA,
		seekerAuthority,
		protocolConfigPDA,
		solana.SystemProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create StartConnection instruction: %w", err)
	}

	latestBlockhash, err := c.RpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

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

// GetWardenPDAForAuthority is a helper to get a warden PDA for a specific public key.
func GetWardenPDAForAuthority(wardenAuthority solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("warden"),
			wardenAuthority.Bytes(),
		},
		ProgramID,
	)
}

// EndConnection sends a transaction to close an active connection.
func (c *Client) EndConnection(wardenAuthority solana.PublicKey) (*solana.Signature, error) {
	seekerAuthority := c.Signer.PublicKey()

	// Derive all PDAs
	seekerPDA, _, err := GetSeekerPDA(seekerAuthority)
	if err != nil {
		return nil, fmt.Errorf("failed to get seeker PDA: %w", err)
	}
	wardenPDA, _, err := GetWardenPDAForAuthority(wardenAuthority)
	if err != nil {
		return nil, fmt.Errorf("failed to get warden PDA: %w", err)
	}
	connectionPDA, _, err := GetConnectionPDA(seekerPDA, wardenPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection PDA: %w", err)
	}

	// Build instruction
	instruction, err := NewEndConnectionInstruction(
		connectionPDA,
		seekerPDA,
		wardenPDA,
		seekerAuthority,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create EndConnection instruction: %w", err)
	}

	// Get latest blockhash
	latestBlockhash, err := c.RpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	// Create and sign transaction
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

	// --- DEBUGGING: PRINT RAW TRANSACTION ---
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		fmt.Println("DEBUG: Failed to marshal tx for debugging:", err)
	} else {
		fmt.Println("\n\n--- DEBUG: RAW TRANSACTION (COPY AND PASTE INTO SOLANA INSPECTOR) ---")
		fmt.Println(base64.StdEncoding.EncodeToString(txBytes))
		fmt.Println("--- END DEBUG ---\\n\n")
	}
	// --- END DEBUGGING ---

	// Send transaction
	sig, err := c.RpcClient.SendTransaction(context.Background(), tx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return &sig, nil
}


// ClaimEarnings sends a transaction for a warden to claim their accumulated earnings.
func (c *Client) ClaimEarnings(usePrivate bool) (*solana.Signature, error) {
	wardenAuthority := c.Signer.PublicKey()

	// Derive PDAs
	wardenPDA, _, err := c.GetWardenPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get warden PDA: %w", err)
	}
	solVaultPDA, _, err := c.GetSolVaultPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get sol_vault PDA: %w", err)
	}

	// Build instruction
	instruction, err := NewClaimEarningsInstruction(
		usePrivate,
		wardenPDA,
		wardenAuthority,
		solVaultPDA,
		solana.SystemProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ClaimEarnings instruction: %w", err)
	}

	// Get latest blockhash
	latestBlockhash, err := c.RpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	// Create and sign transaction
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

	// Send transaction
	sig, err := c.RpcClient.SendTransaction(context.Background(), tx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return &sig, nil
}

// GetArkhamMintPDA returns the PDA for the protocol's ARKHAM token mint.
func (c *Client) GetArkhamMintPDA() (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("arkham_mint"),
		},
		ProgramID,
	)
}

// GetMintAuthorityPDA returns the PDA for the ARKHAM token's mint authority.
func (c *Client) GetMintAuthorityPDA() (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("arkham"),
			[]byte("mint"),
			[]byte("authority"),
		},
		ProgramID,
	)
}

// ClaimArkhamTokens sends a transaction for a warden to claim their earned ARKHAM tokens.
func (c *Client) ClaimArkhamTokens() (*solana.Signature, error) {
	wardenAuthority := c.Signer.PublicKey()

	// Derive all PDAs
	wardenPDA, _, err := c.GetWardenPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get warden PDA: %w", err)
	}
	protocolConfigPDA, _, err := c.GetProtocolConfigPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get protocol_config PDA: %w", err)
	}
	arkhamMintPDA, _, err := c.GetArkhamMintPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get arkham_mint PDA: %w", err)
	}
	mintAuthorityPDA, _, err := c.GetMintAuthorityPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get mint_authority PDA: %w", err)
	}

	// Find the Warden's Associated Token Account for the ARKHAM mint
	wardenAta, _, err := solana.FindAssociatedTokenAddress(
		wardenAuthority,
		arkhamMintPDA,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to find warden's ARKHAM ATA: %w", err)
	}

	// Build instruction
	instruction, err := NewClaimArkhamTokensInstruction(
		wardenPDA,
		wardenAuthority,
		protocolConfigPDA,
		arkhamMintPDA,
		wardenAta,
		mintAuthorityPDA,
		solana.TokenProgramID,
		AssociatedTokenProgramID,
		solana.SystemProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ClaimArkhamTokens instruction: %w", err)
	}

	// Get latest blockhash
	latestBlockhash, err := c.RpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	// Create and sign transaction
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

	// Send transaction
	sig, err := c.RpcClient.SendTransaction(context.Background(), tx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return &sig, nil
}

// FetchWardenAccount fetches and parses the on-chain Warden account data.
func (c *Client) FetchWardenAccount() (*Warden, error) {
	wardenPDA, _, err := c.GetWardenPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to get warden PDA: %w", err)
	}

	resp, err := c.RpcClient.GetAccountInfoWithOpts(context.Background(), wardenPDA, &rpc.GetAccountInfoOpts{
		Commitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get warden account info: %w", err)
	}
	if resp.Value == nil {
		return nil, fmt.Errorf("warden account not found on-chain")
	}

	warden, err := ParseAccount_Warden(resp.Value.Data.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("failed to parse warden account data: %w", err)
	}

	return warden, nil
}

// FetchSeekerAccount fetches and parses the on-chain Seeker account data.
func (c *Client) FetchSeekerAccount() (*Seeker, error) {
	seekerPDA, _, err := GetSeekerPDA(c.Signer.PublicKey())
	if err != nil {
		return nil, fmt.Errorf("failed to get seeker PDA: %w", err)
	}

	resp, err := c.RpcClient.GetAccountInfoWithOpts(context.Background(), seekerPDA, &rpc.GetAccountInfoOpts{
		Commitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		// If the account is not found, it's not a fatal error.
		// We can treat it as a seeker with a zero balance.
		return &Seeker{
			Authority:     c.Signer.PublicKey(),
			EscrowBalance: 0,
		},
		nil
	}
	if resp.Value == nil {
		return &Seeker{
			Authority:     c.Signer.PublicKey(),
			EscrowBalance: 0,
		},
		nil
	}

	seeker, err := ParseAccount_Seeker(resp.Value.Data.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("failed to parse seeker account data: %w", err)
	}

	return seeker, nil
}

// ConnectionResult wraps a Connection account with its public key.
type ConnectionResult struct {
	PublicKey solana.PublicKey
	Account   Connection
}

// FetchMyConnections fetches Connection accounts specific to the client's signer by filtering locally.
func (c *Client) FetchMyConnections(profileType string) ([]*ConnectionResult, error) {
	// 1. Get all connection accounts, filtering only by the account type discriminator.
	resp, err := c.RpcClient.GetProgramAccountsWithOpts(
		context.Background(),
		ProgramID,
		&rpc.GetProgramAccountsOpts{
			Commitment: rpc.CommitmentConfirmed,
			Filters: []rpc.RPCFilter{
				{
					Memcmp: &rpc.RPCFilterMemcmp{
						Offset: 0, // Discriminator is at the start.
						Bytes:  Account_Connection[:],
					},
				},
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get program accounts for connections: %w", err)
	}

	// 2. Get the user's PDA to filter against locally.
	var userPDA solana.PublicKey
	if profileType == "seeker" {
		userPDA, _, err = GetSeekerPDA(c.Signer.PublicKey())
	} else {
		userPDA, _, err = c.GetWardenPDA()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to derive user PDA for filter: %w", err)
	}

	// 3. Parse and filter the results locally.
	var myConnections []*ConnectionResult
	for _, item := range resp {
		account, err := ParseAccount_Connection(item.Account.Data.GetBinary())
		if err != nil {
			fmt.Printf("Warning: failed to parse a Connection account at %s: %v\n", item.Pubkey.String(), err)
			continue
		}

		// Check if the account's seeker or warden field matches the user's PDA.
		isMatch := false
		if profileType == "seeker" && account.Seeker == userPDA {
			isMatch = true
		} else if profileType == "warden" && account.Warden == userPDA {
			isMatch = true
		}

		if isMatch {
			myConnections = append(myConnections, &ConnectionResult{
				PublicKey: item.Pubkey,
				Account:   *account,
			})
		}
	}
	return myConnections, nil
}
