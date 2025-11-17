package cmd

import (
	"arkham-cli/solana"
	"crypto/sha256"
	"fmt"
	"strconv"

	"github.com/AlecAivazis/survey/v2"
	"github.com/gagliardetto/solana-go"
)

const (
	// TODO: Make this configurable or dynamic
	devnetRpcEndpoint = "https://api.devnet.solana.com"
)

// handleRegistration guides the user through the warden registration process.
func handleRegistration() {
	fmt.Println(promptStyle.Render("\n🚀 Warden Registration"))
	fmt.Println(promptStyle.Render("--------------------------"))

	// 1. Select Stake Token
	stakeTokenStr := ""
	tokenPrompt := &survey.Select{
		Message: "Choose your stake token:",
		Options: []string{"SOL", "USDC"},
		Help:    "Select the token you want to stake. USDT is not yet supported on devnet.",
	}
	survey.AskOne(tokenPrompt, &stakeTokenStr, survey.WithValidator(survey.Required))

	var stakeToken arkham_protocol.StakeToken
	switch stakeTokenStr {
	case "SOL":
		stakeToken = arkham_protocol.StakeToken_Sol
	case "USDC":
		stakeToken = arkham_protocol.StakeToken_Usdc
	default:
		fmt.Println(warningStyle.Render("Invalid token selected."))
		return
	}

	// 2. Enter Stake Amount
	stakeAmountStr := ""
	amountPrompt := &survey.Input{
		Message: fmt.Sprintf("Enter amount of %s to stake:", stakeTokenStr),
		Help:    "This amount will be converted to USD to determine your Warden tier.",
	}
	survey.AskOne(amountPrompt, &stakeAmountStr, survey.WithValidator(survey.Required))

	stakeAmountFloat, err := strconv.ParseFloat(stakeAmountStr, 64)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid amount entered."))
		return
	}

	// Convert to lamports or smallest unit
	var stakeAmountU64 uint64
	if stakeToken == arkham_protocol.StakeToken_Sol {
		stakeAmountU64 = uint64(stakeAmountFloat * float64(solana.LAMPORTS_PER_SOL))
	} else {
		// USDC has 6 decimals
		stakeAmountU64 = uint64(stakeAmountFloat * 1_000_000)
	}

	fmt.Printf("Staking %d smallest units of %s...\n", stakeAmountU64, stakeTokenStr)

	// 3. Create Solana Client
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}

	// 4. Call InitializeWarden
	// Using placeholder values for now. These would be fetched from the node itself.
	peerID := "12D3KooWPlaceholderPeerID123456"
	regionCode := uint8(0) // 0 = US
	ipHash := sha256.Sum256([]byte("127.0.0.1"))

	fmt.Println(promptStyle.Render("\nSending registration transaction... Please wait."))

	sig, err := client.InitializeWarden(
		stakeToken,
		stakeAmountU64,
		peerID,
		regionCode,
		ipHash,
	)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n❌ Registration failed: %v", err)))
		return
	}

	fmt.Println(titleStyle.Render("\n✅ Warden Registration Successful!"))
	fmt.Printf("   Transaction Signature: %s\n", sig.String())
	fmt.Println("   It may take a moment for the transaction to be finalized on the blockchain.")
	fmt.Println("   You can check the status on the Solana Explorer.")
}
