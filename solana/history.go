package arkham_protocol

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	bin "github.com/gagliardetto/binary"
)

var (
	initIdlOnce sync.Once
	initIdlErr  error
	idlData     *IDL
	// Map of event discriminators to event names
	eventNameMap map[[8]byte]string
)

// GenericEvent represents a basic transaction event.
type GenericEvent struct {
	Signature solana.Signature `json:"signature"`
	Timestamp time.Time        `json:"timestamp"`
	Type      string           `json:"type"`
	Amount    uint64           `json:"amount,omitempty"`
	Sender    solana.PublicKey `json:"sender,omitempty"`
	Recipient solana.PublicKey `json:"recipient,omitempty"`
}

// ConnectionEvent represents a completed dVPN connection.
type ConnectionEvent struct {
	Signature solana.Signature `json:"signature"`
	Timestamp time.Time        `json:"timestamp"`
	Duration  uint64           `json:"duration"`
	Bandwidth uint64           `json:"bandwidth"`
	Earnings  uint64           `json:"earnings"`
	Warden    solana.PublicKey `json:"warden"`
	Seeker    solana.PublicKey `json:"seeker"`
}

// HistoryResult holds the categorized history.
type HistoryResult struct {
	SolHistory        []GenericEvent    `json:"solHistory"`
	ArkhamHistory     []GenericEvent    `json:"arkhamHistory"`
	ConnectionHistory []ConnectionEvent `json:"connectionHistory"`
}

// initializeIDL loads and parses the IDL data once
func initializeIDL() error {
	initIdlOnce.Do(func() {
		// Parse the embedded IDL JSON
		idlData, initIdlErr = ParseIDL([]byte(idlJSON))
		if initIdlErr != nil {
			return
		}

		// Build the event name map from discriminators
		eventNameMap = make(map[[8]byte]string)
		for _, event := range idlData.Events {
			var disc [8]byte
			copy(disc[:], event.Discriminator)
			eventNameMap[disc] = event.Name
		}
	})
	return initIdlErr
}

// GetHistory fetches and parses the transaction history for a given public key.
func (c *Client) GetHistory(publicKey solana.PublicKey) (*HistoryResult, error) {
	// Initialize IDL if not already done
	if err := initializeIDL(); err != nil {
		return nil, fmt.Errorf("failed to initialize IDL: %w", err)
	}

	result := &HistoryResult{
		SolHistory:        make([]GenericEvent, 0),
		ArkhamHistory:     make([]GenericEvent, 0),
		ConnectionHistory: make([]ConnectionEvent, 0),
	}

	// Fetch transaction signatures for the public key
	ctx := context.Background()
	limit := 1000 // Maximum allowed by Solana RPC
	
	signatures, err := c.RpcClient.GetSignaturesForAddressWithOpts(
		ctx,
		publicKey,
		&rpc.GetSignaturesForAddressOpts{
			Limit:      &limit,
			Commitment: rpc.CommitmentConfirmed,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transaction signatures: %w", err)
	}

	if len(signatures) == 0 {
		// No transactions found, return empty result
		return result, nil
	}

	// Use a mutex to protect concurrent writes to result slices
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process transactions concurrently in batches
	batchSize := 10
	for i := 0; i < len(signatures); i += batchSize {
		end := i + batchSize
		if end > len(signatures) {
			end = len(signatures)
		}

		for j := i; j < end; j++ {
			wg.Add(1)
			go func(sigInfo *rpc.TransactionSignature) {
				defer wg.Done()
				
				// Fetch full transaction details
				version := uint64(0)
				tx, err := c.RpcClient.GetTransaction(
					ctx,
					sigInfo.Signature,
					&rpc.GetTransactionOpts{
						Encoding:                       solana.EncodingBase64,
						Commitment:                     rpc.CommitmentConfirmed,
						MaxSupportedTransactionVersion: &version,
					},
				)
				if err != nil {
					// Log error but continue processing other transactions
					fmt.Printf("Warning: failed to fetch transaction %s: %v\n", sigInfo.Signature, err)
					return
				}

				// Parse the transaction
				parseTransactionForHistory(tx, publicKey, result, &mu)
			}(signatures[j])
		}
		
		// Wait for current batch to complete before starting next batch
		wg.Wait()
	}

	return result, nil
}

// parseTransactionForHistory parses transaction data to build history
func parseTransactionForHistory(tx *rpc.GetTransactionResult, self solana.PublicKey, result *HistoryResult, mu *sync.Mutex) {
	if tx == nil || tx.Meta == nil {
		return
	}

	// Extract timestamp
	var timestamp time.Time
	if tx.BlockTime != nil {
		timestamp = tx.BlockTime.Time()
	} else {
		timestamp = time.Now()
	}

	// Get the transaction signature
	var signature solana.Signature
	if parsed, err := tx.Transaction.GetTransaction(); err == nil && len(parsed.Signatures) > 0 {
		signature = parsed.Signatures[0]
	}

	// Parse program logs for Arkham protocol events
	if tx.Meta.LogMessages != nil {
		parseArkhamEvents(tx, self, timestamp, signature, result, mu)
	}

	// Parse SOL transfers from transaction instructions
	if tx.Transaction != nil {
		parseSolTransfers(tx, self, timestamp, signature, result, mu)
	}

	// Parse token transfers (ARKHAM tokens)
	parseTokenTransfers(tx, self, timestamp, signature, result, mu)
}

// parseArkhamEvents extracts and parses Arkham protocol events from logs
func parseArkhamEvents(tx *rpc.GetTransactionResult, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	for _, log := range tx.Meta.LogMessages {
		// Look for program data logs
		if !strings.Contains(log, "Program data: ") {
			continue
		}

		// Extract the base64-encoded data
		parts := strings.Split(log, "Program data: ")
		if len(parts) < 2 {
			continue
		}

		eventDataB64 := strings.TrimSpace(parts[1])
		eventData, err := base64.StdEncoding.DecodeString(eventDataB64)
		if err != nil {
			continue
		}

		// Check if we have enough data for a discriminator
		if len(eventData) < 8 {
			continue
		}

		// Extract discriminator
		var disc [8]byte
		copy(disc[:], eventData[:8])

		// Look up event name
		eventName, found := eventNameMap[disc]
		if !found {
			continue
		}

		// Parse specific event types
		switch eventName {
		case "ConnectionEnded":
			parseConnectionEndedEvent(eventData, timestamp, signature, result, mu)
		case "ConnectionStarted":
			parseConnectionStartedEvent(eventData, self, timestamp, signature, result, mu)
		case "BandwidthProofSubmitted":
			parseBandwidthProofEvent(eventData, self, timestamp, signature, result, mu)
		case "EscrowDeposited":
			parseEscrowDepositedEvent(eventData, self, timestamp, signature, result, mu)
		case "EarningsClaimed":
			parseEarningsClaimedEvent(eventData, self, timestamp, signature, result, mu)
		case "TokensClaimed":
			parseTokensClaimedEvent(eventData, self, timestamp, signature, result, mu)
		case "WardenRegistered":
			parseWardenRegisteredEvent(eventData, self, timestamp, signature, result, mu)
		}
	}
}

// parseConnectionEndedEvent parses ConnectionEnded events
func parseConnectionEndedEvent(eventData []byte, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	// Skip discriminator and parse event
	event, err := ParseEvent_ConnectionEnded(eventData)
	if err != nil {
		return
	}

	// Calculate duration (we don't have start time here, so set to 0)
	connectionEvent := ConnectionEvent{
		Signature: signature,
		Timestamp: timestamp,
		Duration:  0, // Would need ConnectionStarted event to calculate this
		Bandwidth: event.BandwidthConsumed,
		Earnings:  event.TotalPaid,
		Warden:    event.Warden,
		Seeker:    event.Seeker,
	}

	mu.Lock()
	result.ConnectionHistory = append(result.ConnectionHistory, connectionEvent)
	mu.Unlock()
}

// parseConnectionStartedEvent parses ConnectionStarted events
func parseConnectionStartedEvent(eventData []byte, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	event, err := ParseEvent_ConnectionStarted(eventData)
	if err != nil {
		return
	}

	// Add to Arkham history
	genericEvent := GenericEvent{
		Signature: signature,
		Timestamp: timestamp,
		Type:      "ConnectionStarted",
		Amount:    event.EscrowAmount,
		Sender:    event.Seeker,
		Recipient: event.Warden,
	}

	mu.Lock()
	result.ArkhamHistory = append(result.ArkhamHistory, genericEvent)
	mu.Unlock()
}

// parseBandwidthProofEvent parses BandwidthProofSubmitted events
func parseBandwidthProofEvent(eventData []byte, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	event, err := ParseEvent_BandwidthProofSubmitted(eventData)
	if err != nil {
		return
	}

	genericEvent := GenericEvent{
		Signature: signature,
		Timestamp: timestamp,
		Type:      "BandwidthProofSubmitted",
		Amount:    event.PaymentAmount,
	}

	mu.Lock()
	result.ArkhamHistory = append(result.ArkhamHistory, genericEvent)
	mu.Unlock()
}

// parseEscrowDepositedEvent parses EscrowDeposited events
func parseEscrowDepositedEvent(eventData []byte, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	event, err := ParseEvent_EscrowDeposited(eventData)
	if err != nil {
		return
	}

	if event.Authority != self {
		return
	}

	genericEvent := GenericEvent{
		Signature: signature,
		Timestamp: timestamp,
		Type:      "EscrowDeposited",
		Amount:    event.Amount,
		Sender:    event.Authority,
	}

	mu.Lock()
	result.ArkhamHistory = append(result.ArkhamHistory, genericEvent)
	mu.Unlock()
}

// parseEarningsClaimedEvent parses EarningsClaimed events
func parseEarningsClaimedEvent(eventData []byte, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	event, err := ParseEvent_EarningsClaimed(eventData)
	if err != nil {
		return
	}

	if event.Authority != self {
		return
	}

	genericEvent := GenericEvent{
		Signature: signature,
		Timestamp: timestamp,
		Type:      "EarningsClaimed",
		Amount:    event.Amount,
		Recipient: event.Authority,
	}

	mu.Lock()
	result.ArkhamHistory = append(result.ArkhamHistory, genericEvent)
	mu.Unlock()
}

// parseTokensClaimedEvent parses TokensClaimed events (ARKHAM tokens)
func parseTokensClaimedEvent(eventData []byte, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	event, err := ParseEvent_TokensClaimed(eventData)
	if err != nil {
		return
	}

	if event.Authority != self {
		return
	}

	genericEvent := GenericEvent{
		Signature: signature,
		Timestamp: timestamp,
		Type:      "ArkhamTokensClaimed",
		Amount:    event.Amount,
		Recipient: event.Authority,
	}

	mu.Lock()
	result.ArkhamHistory = append(result.ArkhamHistory, genericEvent)
	mu.Unlock()
}

// parseWardenRegisteredEvent parses WardenRegistered events
func parseWardenRegisteredEvent(eventData []byte, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	event, err := ParseEvent_WardenRegistered(eventData)
	if err != nil {
		return
	}

	if event.Authority != self {
		return
	}

	genericEvent := GenericEvent{
		Signature: signature,
		Timestamp: timestamp,
		Type:      "WardenRegistered",
		Amount:    event.StakeAmount,
		Sender:    event.Authority,
	}

	mu.Lock()
	result.ArkhamHistory = append(result.ArkhamHistory, genericEvent)
	mu.Unlock()
}

// parseSolTransfers extracts SOL transfers from transaction instructions
func parseSolTransfers(tx *rpc.GetTransactionResult, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	if tx.Transaction == nil {
		return
	}

	parsed, err := tx.Transaction.GetTransaction()
	if err != nil {
		return
	}

	// Iterate through instructions looking for System Program transfers
	for _, instr := range parsed.Message.Instructions {
		// Get the program ID
		programIdx := instr.ProgramIDIndex
		if int(programIdx) >= len(parsed.Message.AccountKeys) {
			continue
		}
		programID := parsed.Message.AccountKeys[programIdx]

		// Check if it's the System Program
		if programID != solana.SystemProgramID {
			continue
		}

		// Parse instruction data
		if len(instr.Data) < 4 {
			continue
		}

		// Decode instruction type (first 4 bytes for System Program)
		decoder := bin.NewBorshDecoder(instr.Data)
		var instrType uint32
		if err := decoder.Decode(&instrType); err != nil {
			continue
		}

		// 2 = Transfer instruction
		if instrType != 2 {
			continue
		}

		// Parse transfer amount
		var amount uint64
		if err := decoder.Decode(&amount); err != nil {
			continue
		}

		// Get sender and recipient from accounts
		if len(instr.Accounts) < 2 {
			continue
		}

		fromIdx := instr.Accounts[0]
		toIdx := instr.Accounts[1]

		if int(fromIdx) >= len(parsed.Message.AccountKeys) || int(toIdx) >= len(parsed.Message.AccountKeys) {
			continue
		}

		from := parsed.Message.AccountKeys[fromIdx]
		to := parsed.Message.AccountKeys[toIdx]

		// Only include if user is involved
		if from != self && to != self {
			continue
		}

		eventType := "SOLTransferSent"
		sender := from
		recipient := to

		if to == self {
			eventType = "SOLTransferReceived"
		}

		genericEvent := GenericEvent{
			Signature: signature,
			Timestamp: timestamp,
			Type:      eventType,
			Amount:    amount,
			Sender:    sender,
			Recipient: recipient,
		}

		mu.Lock()
		result.SolHistory = append(result.SolHistory, genericEvent)
		mu.Unlock()
	}
}

// parseTokenTransfers extracts token transfers (for ARKHAM tokens)
func parseTokenTransfers(tx *rpc.GetTransactionResult, self solana.PublicKey, timestamp time.Time, signature solana.Signature, result *HistoryResult, mu *sync.Mutex) {
	if tx.Transaction == nil || tx.Meta == nil {
		return
	}

	parsed, err := tx.Transaction.GetTransaction()
	if err != nil {
		return
	}

	// Get ARKHAM mint PDA
	arkhamMintPDA, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("arkham_mint")},
		ProgramID,
	)
	if err != nil {
		return
	}

	// Look for token transfers in pre and post token balances
	if tx.Meta.PreTokenBalances != nil && tx.Meta.PostTokenBalances != nil {
		for _, postBalance := range tx.Meta.PostTokenBalances {
			// Check if it's ARKHAM token
			if postBalance.Mint != arkhamMintPDA {
				continue
			}

			// Find corresponding pre-balance
			var preAmount uint64 = 0
			for _, preBalance := range tx.Meta.PreTokenBalances {
				if preBalance.AccountIndex == postBalance.AccountIndex {
					if preBalance.UiTokenAmount.Amount != "" {
						fmt.Sscanf(preBalance.UiTokenAmount.Amount, "%d", &preAmount)
					}
					break
				}
			}

			// Calculate transfer amount
			var postAmount uint64 = 0
			if postBalance.UiTokenAmount.Amount != "" {
				fmt.Sscanf(postBalance.UiTokenAmount.Amount, "%d", &postAmount)
			}

			if postAmount == preAmount {
				continue
			}

			// Get the token account owner
			accountIdx := postBalance.AccountIndex
			if int(accountIdx) >= len(parsed.Message.AccountKeys) {
				continue
			}

			var amount uint64
			var eventType string

			if postAmount > preAmount {
				amount = postAmount - preAmount
				eventType = "ArkhamTokenReceived"
			} else {
				amount = preAmount - postAmount
				eventType = "ArkhamTokenSent"
			}

			genericEvent := GenericEvent{
				Signature: signature,
				Timestamp: timestamp,
				Type:      eventType,
				Amount:    amount,
			}

			mu.Lock()
			result.ArkhamHistory = append(result.ArkhamHistory, genericEvent)
			mu.Unlock()
		}
	}
}

// Embedded IDL JSON - Complete arkham_protocol.json content
const idlJSON = `{
  "address": "B85X9aTrpWAdi1xhLvPmDPuYmfz5YdMd9X8qr7uU4H18",
  "metadata": {
    "name": "arkham_protocol",
    "version": "0.1.0",
    "spec": "0.1.0",
    "description": "Created with Anchor"
  },
  "instructions": [
    {
      "name": "claim_arkham_tokens",
      "discriminator": [
        180,
        14,
        137,
        225,
        247,
        246,
        242,
        200
      ],
      "accounts": [
        {
          "name": "warden",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  119,
                  97,
                  114,
                  100,
                  101,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "authority"
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true,
          "relations": [
            "warden"
          ]
        },
        {
          "name": "protocol_config",
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "arkham_mint",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  97,
                  114,
                  107,
                  104,
                  97,
                  109,
                  95,
                  109,
                  105,
                  110,
                  116
                ]
              }
            ]
          }
        },
        {
          "name": "warden_arkham_token_account",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "account",
                "path": "authority"
              },
              {
                "kind": "const",
                "value": [
                  6,
                  221,
                  246,
                  225,
                  215,
                  101,
                  161,
                  147,
                  217,
                  203,
                  225,
                  70,
                  206,
                  235,
                  121,
                  172,
                  28,
                  180,
                  133,
                  237,
                  95,
                  91,
                  55,
                  145,
                  58,
                  140,
                  245,
                  133,
                  126,
                  255,
                  0,
                  169
                ]
              },
              {
                "kind": "account",
                "path": "arkham_mint"
              }
            ],
            "program": {
              "kind": "const",
              "value": [
                140,
                151,
                37,
                143,
                78,
                36,
                137,
                241,
                187,
                61,
                16,
                41,
                20,
                142,
                13,
                131,
                11,
                90,
                19,
                153,
                218,
                255,
                16,
                132,
                4,
                142,
                123,
                216,
                219,
                233,
                248,
                89
              ]
            }
          }
        },
        {
          "name": "mint_authority",
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  97,
                  114,
                  107,
                  104,
                  97,
                  109
                ]
              },
              {
                "kind": "const",
                "value": [
                  109,
                  105,
                  110,
                  116
                ]
              },
              {
                "kind": "const",
                "value": [
                  97,
                  117,
                  116,
                  104,
                  111,
                  114,
                  105,
                  116,
                  121
                ]
              }
            ]
          }
        },
        {
          "name": "token_program",
          "address": "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
        },
        {
          "name": "associated_token_program",
          "address": "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        }
      ],
      "args": []
    },
    {
      "name": "claim_earnings",
      "discriminator": [
        49,
        99,
        161,
        170,
        22,
        233,
        54,
        140
      ],
      "accounts": [
        {
          "name": "warden",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  119,
                  97,
                  114,
                  100,
                  101,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "authority"
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true,
          "relations": [
            "warden"
          ]
        },
        {
          "name": "sol_vault",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  115,
                  111,
                  108,
                  95,
                  118,
                  97,
                  117,
                  108,
                  116
                ]
              }
            ]
          }
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        }
      ],
      "args": [
        {
          "name": "use_private",
          "type": "bool"
        }
      ]
    },
    {
      "name": "claim_unstake",
      "discriminator": [
        172,
        113,
        117,
        178,
        223,
        245,
        247,
        118
      ],
      "accounts": [
        {
          "name": "warden",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  119,
                  97,
                  114,
                  100,
                  101,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "authority"
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true,
          "relations": [
            "warden"
          ]
        },
        {
          "name": "sol_vault",
          "docs": [
            "The protocol's SOL vault (PDA)"
          ],
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  115,
                  111,
                  108,
                  95,
                  118,
                  97,
                  117,
                  108,
                  116
                ]
              }
            ]
          }
        },
        {
          "name": "usdc_vault",
          "writable": true
        },
        {
          "name": "usdt_vault",
          "writable": true
        },
        {
          "name": "stake_to_account",
          "writable": true
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        },
        {
          "name": "token_program",
          "address": "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
        }
      ],
      "args": []
    },
    {
      "name": "close_protocol_config",
      "discriminator": [
        203,
        147,
        4,
        67,
        17,
        28,
        203,
        219
      ],
      "accounts": [
        {
          "name": "protocol_config",
          "docs": [
            "Protocol config account to close - using AccountInfo to avoid deserialization"
          ],
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "authority",
          "docs": [
            "The authority that should match the one stored in the account"
          ],
          "writable": true,
          "signer": true
        },
        {
          "name": "receiver",
          "docs": [
            "Receiver of the rent (can be the authority or another account)"
          ],
          "writable": true
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        }
      ],
      "args": []
    },
    {
      "name": "deposit_escrow",
      "discriminator": [
        226,
        112,
        158,
        176,
        178,
        118,
        153,
        128
      ],
      "accounts": [
        {
          "name": "seeker",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  115,
                  101,
                  101,
                  107,
                  101,
                  114
                ]
              },
              {
                "kind": "account",
                "path": "authority"
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        }
      ],
      "args": [
        {
          "name": "amount",
          "type": "u64"
        },
        {
          "name": "use_private",
          "type": "bool"
        }
      ]
    },
    {
      "name": "distribute_subsidies",
      "discriminator": [
        38,
        141,
        106,
        248,
        234,
        99,
        180,
        91
      ],
      "accounts": [
        {
          "name": "protocol_config",
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "treasury",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "account",
                "path": "treasury_authority"
              },
              {
                "kind": "const",
                "value": [
                  6,
                  221,
                  246,
                  225,
                  215,
                  101,
                  161,
                  147,
                  217,
                  203,
                  225,
                  70,
                  206,
                  235,
                  121,
                  172,
                  28,
                  180,
                  133,
                  237,
                  95,
                  91,
                  55,
                  145,
                  58,
                  140,
                  245,
                  133,
                  126,
                  255,
                  0,
                  169
                ]
              },
              {
                "kind": "account",
                "path": "arkham_mint"
              }
            ],
            "program": {
              "kind": "const",
              "value": [
                140,
                151,
                37,
                143,
                78,
                36,
                137,
                241,
                187,
                61,
                16,
                41,
                20,
                142,
                13,
                131,
                11,
                90,
                19,
                153,
                218,
                255,
                16,
                132,
                4,
                142,
                123,
                216,
                219,
                233,
                248,
                89
              ]
            }
          }
        },
        {
          "name": "arkham_mint"
        },
        {
          "name": "treasury_authority"
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        }
      ],
      "args": [
        {
          "name": "warden_keys",
          "type": {
            "vec": "pubkey"
          }
        },
        {
          "name": "subsidy_amounts",
          "type": {
            "vec": "u64"
          }
        }
      ]
    },
    {
      "name": "end_connection",
      "discriminator": [
        145,
        116,
        162,
        199,
        86,
        180,
        63,
        42
      ],
      "accounts": [
        {
          "name": "connection",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  99,
                  111,
                  110,
                  110,
                  101,
                  99,
                  116,
                  105,
                  111,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "seeker"
              },
              {
                "kind": "account",
                "path": "warden"
              }
            ]
          }
        },
        {
          "name": "seeker",
          "writable": true
        },
        {
          "name": "warden",
          "writable": true
        },
        {
          "name": "seeker_authority",
          "writable": true,
          "signer": true
        }
      ],
      "args": []
    },
    {
      "name": "initialize",
      "discriminator": [
        175,
        175,
        109,
        31,
        13,
        152,
        155,
        237
      ],
      "accounts": [
        {
          "name": "dummy_account",
          "docs": [
            "Simple initialization for testing - just log a message"
          ],
          "signer": true
        }
      ],
      "args": []
    },
    {
      "name": "initialize_arkham_mint",
      "discriminator": [
        199,
        33,
        247,
        30,
        147,
        49,
        100,
        72
      ],
      "accounts": [
        {
          "name": "arkham_mint",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  97,
                  114,
                  107,
                  104,
                  97,
                  109,
                  95,
                  109,
                  105,
                  110,
                  116
                ]
              }
            ]
          }
        },
        {
          "name": "mint_authority",
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  97,
                  114,
                  107,
                  104,
                  97,
                  109
                ]
              },
              {
                "kind": "const",
                "value": [
                  109,
                  105,
                  110,
                  116
                ]
              },
              {
                "kind": "const",
                "value": [
                  97,
                  117,
                  116,
                  104,
                  111,
                  114,
                  105,
                  116,
                  121
                ]
              }
            ]
          }
        },
        {
          "name": "protocol_config",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        },
        {
          "name": "token_program",
          "address": "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        }
      ],
      "args": []
    },
    {
      "name": "initialize_protocol_config",
      "discriminator": [
        28,
        50,
        43,
        233,
        244,
        98,
        123,
        118
      ],
      "accounts": [
        {
          "name": "protocol_config",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "treasury"
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        }
      ],
      "args": [
        {
          "name": "base_rate_per_mb",
          "type": "u64"
        },
        {
          "name": "protocol_fee_bps",
          "type": "u16"
        },
        {
          "name": "tier_thresholds",
          "type": {
            "array": [
              "u64",
              3
            ]
          }
        },
        {
          "name": "tier_multipliers",
          "type": {
            "array": [
              "u16",
              3
            ]
          }
        },
        {
          "name": "tokens_per_5gb",
          "type": "u64"
        },
        {
          "name": "geo_premiums",
          "type": {
            "vec": {
              "defined": {
                "name": "GeoPremium"
              }
            }
          }
        },
        {
          "name": "oracle_authority",
          "type": "pubkey"
        }
      ]
    },
    {
      "name": "initialize_warden",
      "discriminator": [
        208,
        228,
        42,
        148,
        121,
        54,
        243,
        65
      ],
      "accounts": [
        {
          "name": "warden",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  119,
                  97,
                  114,
                  100,
                  101,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "authority"
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        },
        {
          "name": "protocol_config",
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "instructions_sysvar",
          "address": "Sysvar1nstructions1111111111111111111111111"
        },
        {
          "name": "stake_from_account",
          "writable": true
        },
        {
          "name": "sol_vault",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  115,
                  111,
                  108,
                  95,
                  118,
                  97,
                  117,
                  108,
                  116
                ]
              }
            ]
          }
        },
        {
          "name": "usdc_vault",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "account",
                "path": "sol_vault"
              },
              {
                "kind": "const",
                "value": [
                  6,
                  221,
                  246,
                  225,
                  215,
                  101,
                  161,
                  147,
                  217,
                  203,
                  225,
                  70,
                  206,
                  235,
                  121,
                  172,
                  28,
                  180,
                  133,
                  237,
                  95,
                  91,
                  55,
                  145,
                  58,
                  140,
                  245,
                  133,
                  126,
                  255,
                  0,
                  169
                ]
              },
              {
                "kind": "account",
                "path": "usdc_mint"
              }
            ],
            "program": {
              "kind": "const",
              "value": [
                140,
                151,
                37,
                143,
                78,
                36,
                137,
                241,
                187,
                61,
                16,
                41,
                20,
                142,
                13,
                131,
                11,
                90,
                19,
                153,
                218,
                255,
                16,
                132,
                4,
                142,
                123,
                216,
                219,
                233,
                248,
                89
              ]
            }
          }
        },
        {
          "name": "usdt_vault",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "account",
                "path": "sol_vault"
              },
              {
                "kind": "const",
                "value": [
                  6,
                  221,
                  246,
                  225,
                  215,
                  101,
                  161,
                  147,
                  217,
                  203,
                  225,
                  70,
                  206,
                  235,
                  121,
                  172,
                  28,
                  180,
                  133,
                  237,
                  95,
                  91,
                  55,
                  145,
                  58,
                  140,
                  245,
                  133,
                  126,
                  255,
                  0,
                  169
                ]
              },
              {
                "kind": "account",
                "path": "usdt_mint"
              }
            ],
            "program": {
              "kind": "const",
              "value": [
                140,
                151,
                37,
                143,
                78,
                36,
                137,
                241,
                187,
                61,
                16,
                41,
                20,
                142,
                13,
                131,
                11,
                90,
                19,
                153,
                218,
                255,
                16,
                132,
                4,
                142,
                123,
                216,
                219,
                233,
                248,
                89
              ]
            }
          }
        },
        {
          "name": "usdc_mint"
        },
        {
          "name": "usdt_mint"
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        },
        {
          "name": "token_program",
          "address": "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
        },
        {
          "name": "associated_token_program",
          "address": "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
        }
      ],
      "args": [
        {
          "name": "stake_token",
          "type": {
            "defined": {
              "name": "StakeToken"
            }
          }
        },
        {
          "name": "stake_amount",
          "type": "u64"
        },
        {
          "name": "peer_id",
          "type": "string"
        },
        {
          "name": "region_code",
          "type": "u8"
        },
        {
          "name": "ip_hash",
          "type": {
            "array": [
              "u8",
              32
            ]
          }
        },
        {
          "name": "price",
          "type": "u64"
        },
        {
          "name": "timestamp",
          "type": "i64"
        },
        {
          "name": "signature",
          "type": {
            "array": [
              "u8",
              64
            ]
          }
        }
      ]
    },
    {
      "name": "migrate_protocol_config",
      "discriminator": [
        240,
        133,
        241,
        218,
        118,
        253,
        139,
        28
      ],
      "accounts": [
        {
          "name": "protocol_config",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        },
        {
          "name": "new_oracle_authority",
          "docs": [
            "New oracle authority to set"
          ]
        }
      ],
      "args": []
    },
    {
      "name": "start_connection",
      "discriminator": [
        27,
        34,
        208,
        176,
        138,
        230,
        238,
        74
      ],
      "accounts": [
        {
          "name": "connection",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  99,
                  111,
                  110,
                  110,
                  101,
                  99,
                  116,
                  105,
                  111,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "seeker"
              },
              {
                "kind": "account",
                "path": "warden"
              }
            ]
          }
        },
        {
          "name": "seeker",
          "writable": true
        },
        {
          "name": "warden",
          "writable": true
        },
        {
          "name": "seeker_authority",
          "writable": true,
          "signer": true
        },
        {
          "name": "protocol_config"
        },
        {
          "name": "system_program",
          "address": "11111111111111111111111111111111"
        }
      ],
      "args": [
        {
          "name": "estimated_mb",
          "type": "u64"
        }
      ]
    },
    {
      "name": "submit_bandwidth_proof",
      "discriminator": [
        98,
        96,
        38,
        149,
        163,
        54,
        248,
        15
      ],
      "accounts": [
        {
          "name": "connection",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  99,
                  111,
                  110,
                  110,
                  101,
                  99,
                  116,
                  105,
                  111,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "connection.seeker",
                "account": "Connection"
              },
              {
                "kind": "account",
                "path": "connection.warden",
                "account": "Connection"
              }
            ]
          }
        },
        {
          "name": "warden",
          "writable": true,
          "relations": [
            "connection"
          ]
        },
        {
          "name": "seeker",
          "writable": true,
          "relations": [
            "connection"
          ]
        },
        {
          "name": "protocol_config"
        },
        {
          "name": "instructions_sysvar",
          "address": "Sysvar1nstructions1111111111111111111111111"
        },
        {
          "name": "submitter",
          "docs": [
            "Either seeker or warden can submit proofs"
          ],
          "signer": true
        }
      ],
      "args": [
        {
          "name": "mb_consumed",
          "type": "u64"
        },
        {
          "name": "timestamp",
          "type": "i64"
        },
        {
          "name": "seeker_signature",
          "type": {
            "array": [
              "u8",
              64
            ]
          }
        },
        {
          "name": "warden_signature",
          "type": {
            "array": [
              "u8",
              64
            ]
          }
        }
      ]
    },
    {
      "name": "unstake_warden",
      "discriminator": [
        224,
        104,
        75,
        109,
        242,
        42,
        150,
        156
      ],
      "accounts": [
        {
          "name": "warden",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  119,
                  97,
                  114,
                  100,
                  101,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "authority"
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true,
          "relations": [
            "warden"
          ]
        }
      ],
      "args": []
    },
    {
      "name": "update_premium_pool_rankings",
      "discriminator": [
        159,
        191,
        184,
        117,
        40,
        215,
        186,
        36
      ],
      "accounts": [
        {
          "name": "protocol_config",
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        }
      ],
      "args": [
        {
          "name": "top_wardens",
          "type": {
            "vec": "pubkey"
          }
        }
      ]
    },
    {
      "name": "update_protocol_config",
      "discriminator": [
        197,
        97,
        123,
        54,
        221,
        168,
        11,
        135
      ],
      "accounts": [
        {
          "name": "protocol_config",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        }
      ],
      "args": [
        {
          "name": "new_base_rate_per_mb",
          "type": {
            "option": "u64"
          }
        },
        {
          "name": "new_protocol_fee_bps",
          "type": {
            "option": "u16"
          }
        },
        {
          "name": "new_tier_thresholds",
          "type": {
            "option": {
              "array": [
                "u64",
                3
              ]
            }
          }
        },
        {
          "name": "new_tier_multipliers",
          "type": {
            "option": {
              "array": [
                "u16",
                3
              ]
            }
          }
        },
        {
          "name": "new_tokens_per_5gb",
          "type": {
            "option": "u64"
          }
        },
        {
          "name": "new_geo_premiums",
          "type": {
            "option": {
              "vec": {
                "defined": {
                  "name": "GeoPremium"
                }
              }
            }
          }
        },
        {
          "name": "new_reputation_updater",
          "type": {
            "option": "pubkey"
          }
        },
        {
          "name": "new_oracle_authority",
          "type": {
            "option": "pubkey"
          }
        }
      ]
    },
    {
      "name": "update_reputation",
      "discriminator": [
        194,
        220,
        43,
        201,
        54,
        209,
        49,
        178
      ],
      "accounts": [
        {
          "name": "warden",
          "writable": true,
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  119,
                  97,
                  114,
                  100,
                  101,
                  110
                ]
              },
              {
                "kind": "account",
                "path": "warden_authority"
              }
            ]
          }
        },
        {
          "name": "protocol_config",
          "pda": {
            "seeds": [
              {
                "kind": "const",
                "value": [
                  112,
                  114,
                  111,
                  116,
                  111,
                  99,
                  111,
                  108,
                  95,
                  99,
                  111,
                  110,
                  102,
                  105,
                  103
                ]
              }
            ]
          }
        },
        {
          "name": "warden_authority",
          "writable": true
        },
        {
          "name": "authority",
          "writable": true,
          "signer": true
        }
      ],
      "args": [
        {
          "name": "connection_success",
          "type": "bool"
        },
        {
          "name": "uptime_report",
          "type": "u16"
        }
      ]
    }
  ],
  "accounts": [
    {
      "name": "Connection",
      "discriminator": [
        209,
        186,
        115,
        58,
        36,
        236,
        179,
        10
      ]
    },
    {
      "name": "ProtocolConfig",
      "discriminator": [
        207,
        91,
        250,
        28,
        152,
        179,
        215,
        209
      ]
    },
    {
      "name": "Seeker",
      "discriminator": [
        106,
        201,
        97,
        118,
        1,
        110,
        224,
        133
      ]
    },
    {
      "name": "Warden",
      "discriminator": [
        73,
        11,
        82,
        46,
        202,
        0,
        179,
        133
      ]
    }
  ],
  "events": [
    {
      "name": "ArkhamMintInitialized",
      "discriminator": [
        24,
        177,
        72,
        228,
        14,
        191,
        164,
        189
      ]
    },
    {
      "name": "BandwidthProofSubmitted",
      "discriminator": [
        73,
        164,
        55,
        174,
        238,
        117,
        228,
        8
      ]
    },
    {
      "name": "ConnectionEnded",
      "discriminator": [
        155,
        105,
        4,
        133,
        186,
        109,
        217,
        137
      ]
    },
    {
      "name": "ConnectionStarted",
      "discriminator": [
        253,
        72,
        159,
        233,
        126,
        6,
        248,
        3
      ]
    },
    {
      "name": "EarningsClaimed",
      "discriminator": [
        106,
        170,
        154,
        105,
        21,
        43,
        189,
        97
      ]
    },
    {
      "name": "EscrowDeposited",
      "discriminator": [
        28,
        193,
        105,
        27,
        40,
        101,
        65,
        211
      ]
    },
    {
      "name": "PremiumPoolRankingsUpdated",
      "discriminator": [
        109,
        220,
        21,
        16,
        82,
        23,
        122,
        27
      ]
    },
    {
      "name": "ProtocolConfigInitialized",
      "discriminator": [
        243,
        69,
        27,
        238,
        111,
        169,
        87,
        231
      ]
    },
    {
      "name": "ProtocolConfigUpdated",
      "discriminator": [
        20,
        99,
        32,
        237,
        111,
        86,
        195,
        199
      ]
    },
    {
      "name": "ReputationUpdated",
      "discriminator": [
        26,
        36,
        187,
        150,
        235,
        90,
        106,
        89
      ]
    },
    {
      "name": "SubsidiesDistributed",
      "discriminator": [
        133,
        199,
        129,
        213,
        115,
        186,
        210,
        0
      ]
    },
    {
      "name": "TokensClaimed",
      "discriminator": [
        25,
        128,
        244,
        55,
        241,
        136,
        200,
        91
      ]
    },
    {
      "name": "UnstakeRequested",
      "discriminator": [
        21,
        253,
        177,
        85,
        129,
        206,
        42,
        152
      ]
    },
    {
      "name": "WardenRegistered",
      "discriminator": [
        131,
        190,
        122,
        62,
        145,
        152,
        187,
        227
      ]
    },
    {
      "name": "WardenUnstaked",
      "discriminator": [
        150,
        7,
        246,
        105,
        220,
        235,
        137,
        32
      ]
    }
  ],
  "errors": [
    {
      "code": 6000,
      "name": "InvalidInstructionsSysvar",
      "msg": "Invalid Instructions sysvar account"
    },
    {
      "code": 6001,
      "name": "Ed25519InstructionNotFound",
      "msg": "Ed25519Program instruction not found at expected index"
    },
    {
      "code": 6002,
      "name": "InvalidEd25519Instruction",
      "msg": "Instruction is not an Ed25519Program instruction"
    },
    {
      "code": 6003,
      "name": "InvalidEd25519Data",
      "msg": "Ed25519Program instruction data is invalid or too short"
    },
    {
      "code": 6004,
      "name": "SignatureMismatch",
      "msg": "Signature in Ed25519 instruction doesn't match expected signature"
    },
    {
      "code": 6005,
      "name": "PublicKeyMismatch",
      "msg": "Public key in Ed25519 instruction doesn't match oracle authority"
    },
    {
      "code": 6006,
      "name": "MessageMismatch",
      "msg": "Message in Ed25519 instruction doesn't match expected message"
    }
  ],
  "types": [
    {
      "name": "ArkhamMintInitialized",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "mint",
            "type": "pubkey"
          }
        ]
      }
    },
    {
      "name": "BandwidthProof",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "timestamp",
            "type": "i64"
          },
          {
            "name": "mb_consumed",
            "type": "u64"
          },
          {
            "name": "seeker_signature",
            "type": {
              "array": [
                "u8",
                64
              ]
            }
          },
          {
            "name": "warden_signature",
            "type": {
              "array": [
                "u8",
                64
              ]
            }
          }
        ]
      }
    },
    {
      "name": "BandwidthProofSubmitted",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "connection",
            "type": "pubkey"
          },
          {
            "name": "mb_consumed",
            "type": "u64"
          },
          {
            "name": "payment_amount",
            "type": "u64"
          },
          {
            "name": "arkham_earned",
            "type": "u64"
          }
        ]
      }
    },
    {
      "name": "Connection",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "seeker",
            "type": "pubkey"
          },
          {
            "name": "warden",
            "type": "pubkey"
          },
          {
            "name": "started_at",
            "type": "i64"
          },
          {
            "name": "last_proof_at",
            "type": "i64"
          },
          {
            "name": "bandwidth_consumed",
            "type": "u64"
          },
          {
            "name": "bandwidth_proofs",
            "type": {
              "vec": {
                "defined": {
                  "name": "BandwidthProof"
                }
              }
            }
          },
          {
            "name": "amount_escrowed",
            "type": "u64"
          },
          {
            "name": "amount_paid",
            "type": "u64"
          },
          {
            "name": "rate_per_mb",
            "type": "u64"
          },
          {
            "name": "warden_multiplier",
            "type": "u16"
          }
        ]
      }
    },
    {
      "name": "ConnectionEnded",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "seeker",
            "type": "pubkey"
          },
          {
            "name": "warden",
            "type": "pubkey"
          },
          {
            "name": "bandwidth_consumed",
            "type": "u64"
          },
          {
            "name": "total_paid",
            "type": "u64"
          },
          {
            "name": "refunded",
            "type": "u64"
          }
        ]
      }
    },
    {
      "name": "ConnectionStarted",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "seeker",
            "type": "pubkey"
          },
          {
            "name": "warden",
            "type": "pubkey"
          },
          {
            "name": "estimated_mb",
            "type": "u64"
          },
          {
            "name": "rate_per_mb",
            "type": "u64"
          },
          {
            "name": "escrow_amount",
            "type": "u64"
          }
        ]
      }
    },
    {
      "name": "EarningsClaimed",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "amount",
            "type": "u64"
          },
          {
            "name": "use_private",
            "type": "bool"
          }
        ]
      }
    },
    {
      "name": "EscrowDeposited",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "amount",
            "type": "u64"
          },
          {
            "name": "use_private",
            "type": "bool"
          }
        ]
      }
    },
    {
      "name": "GeoPremium",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "region_code",
            "type": "u8"
          },
          {
            "name": "premium_bps",
            "type": "u16"
          }
        ]
      }
    },
    {
      "name": "PremiumPoolRankingsUpdated",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "updater",
            "type": "pubkey"
          },
          {
            "name": "top_wardens_count",
            "type": "u32"
          }
        ]
      }
    },
    {
      "name": "ProtocolConfig",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "treasury",
            "type": "pubkey"
          },
          {
            "name": "arkham_token_mint",
            "type": "pubkey"
          },
          {
            "name": "oracle_authority",
            "type": "pubkey"
          },
          {
            "name": "base_rate_per_mb",
            "type": "u64"
          },
          {
            "name": "protocol_fee_bps",
            "type": "u16"
          },
          {
            "name": "tier_thresholds",
            "type": {
              "array": [
                "u64",
                3
              ]
            }
          },
          {
            "name": "tier_multipliers",
            "type": {
              "array": [
                "u16",
                3
              ]
            }
          },
          {
            "name": "tokens_per_5gb",
            "type": "u64"
          },
          {
            "name": "geo_premiums",
            "type": {
              "vec": {
                "defined": {
                  "name": "GeoPremium"
                }
              }
            }
          },
          {
            "name": "reputation_updater",
            "type": "pubkey"
          }
        ]
      }
    },
    {
      "name": "ProtocolConfigInitialized",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "base_rate_per_mb",
            "type": "u64"
          },
          {
            "name": "protocol_fee_bps",
            "type": "u16"
          }
        ]
      }
    },
    {
      "name": "ProtocolConfigUpdated",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "new_base_rate_per_mb",
            "type": {
              "option": "u64"
            }
          },
          {
            "name": "new_protocol_fee_bps",
            "type": {
              "option": "u16"
            }
          },
          {
            "name": "new_tier_thresholds",
            "type": {
              "option": {
                "array": [
                  "u64",
                  3
                ]
              }
            }
          },
          {
            "name": "new_tier_multipliers",
            "type": {
              "option": {
                "array": [
                  "u16",
                  3
                ]
              }
            }
          },
          {
            "name": "new_tokens_per_5gb",
            "type": {
              "option": "u64"
            }
          }
        ]
      }
    },
    {
      "name": "ReputationUpdated",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "warden",
            "type": "pubkey"
          },
          {
            "name": "new_score",
            "type": "u32"
          },
          {
            "name": "uptime_report",
            "type": "u16"
          },
          {
            "name": "connection_success",
            "type": "bool"
          }
        ]
      }
    },
    {
      "name": "Seeker",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "escrow_balance",
            "type": "u64"
          },
          {
            "name": "private_escrow",
            "type": {
              "option": "pubkey"
            }
          },
          {
            "name": "total_bandwidth_consumed",
            "type": "u64"
          },
          {
            "name": "total_spent",
            "type": "u64"
          },
          {
            "name": "active_connections",
            "type": "u8"
          },
          {
            "name": "premium_expires_at",
            "type": {
              "option": "i64"
            }
          }
        ]
      }
    },
    {
      "name": "StakeToken",
      "type": {
        "kind": "enum",
        "variants": [
          {
            "name": "Sol"
          },
          {
            "name": "Usdc"
          },
          {
            "name": "Usdt"
          }
        ]
      }
    },
    {
      "name": "SubsidiesDistributed",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "warden_count",
            "type": "u32"
          },
          {
            "name": "total_amount",
            "type": "u64"
          }
        ]
      }
    },
    {
      "name": "Tier",
      "type": {
        "kind": "enum",
        "variants": [
          {
            "name": "Bronze"
          },
          {
            "name": "Silver"
          },
          {
            "name": "Gold"
          }
        ]
      }
    },
    {
      "name": "TokensClaimed",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "amount",
            "type": "u64"
          }
        ]
      }
    },
    {
      "name": "UnstakeRequested",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "requested_at",
            "type": "i64"
          }
        ]
      }
    },
    {
      "name": "Warden",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "peer_id",
            "type": "string"
          },
          {
            "name": "stake_token",
            "type": {
              "defined": {
                "name": "StakeToken"
              }
            }
          },
          {
            "name": "stake_amount",
            "type": "u64"
          },
          {
            "name": "stake_value_usd",
            "type": "u64"
          },
          {
            "name": "tier",
            "type": {
              "defined": {
                "name": "Tier"
              }
            }
          },
          {
            "name": "staked_at",
            "type": "i64"
          },
          {
            "name": "unstake_requested_at",
            "type": {
              "option": "i64"
            }
          },
          {
            "name": "total_bandwidth_served",
            "type": "u64"
          },
          {
            "name": "total_earnings",
            "type": "u64"
          },
          {
            "name": "pending_claims",
            "type": "u64"
          },
          {
            "name": "arkham_tokens_earned",
            "type": "u64"
          },
          {
            "name": "reputation_score",
            "type": "u32"
          },
          {
            "name": "successful_connections",
            "type": "u64"
          },
          {
            "name": "failed_connections",
            "type": "u64"
          },
          {
            "name": "uptime_percentage",
            "type": "u16"
          },
          {
            "name": "last_active",
            "type": "i64"
          },
          {
            "name": "region_code",
            "type": "u8"
          },
          {
            "name": "ip_hash",
            "type": {
              "array": [
                "u8",
                32
              ]
            }
          },
          {
            "name": "premium_pool_rank",
            "type": {
              "option": "u16"
            }
          },
          {
            "name": "active_connections",
            "type": "u8"
          }
        ]
      }
    },
    {
      "name": "WardenRegistered",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "tier",
            "type": {
              "defined": {
                "name": "Tier"
              }
            }
          },
          {
            "name": "stake_amount",
            "type": "u64"
          },
          {
            "name": "stake_token",
            "type": {
              "defined": {
                "name": "StakeToken"
              }
            }
          }
        ]
      }
    },
    {
      "name": "WardenUnstaked",
      "type": {
        "kind": "struct",
        "fields": [
          {
            "name": "authority",
            "type": "pubkey"
          },
          {
            "name": "stake_amount",
            "type": "u64"
          },
          {
            "name": "stake_token",
            "type": {
              "defined": {
                "name": "StakeToken"
              }
            }
          }
        ]
      }
    }
  ]
}`