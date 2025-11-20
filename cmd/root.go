package cmd

import (
	"arkham-cli/storage"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	arkham_protocol "arkham-cli/solana"

	"github.com/AlecAivazis/survey/v2"
	figure "github.com/common-nighthawk/go-figure"
	"github.com/gagliardetto/solana-go"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var (
	devnetRpcEndpoint = "https://api.devnet.solana.com"
)

var rootCmd = &cobra.Command{
	Use:   "arkham-cli",
	Short: "Arkham CLI helps you join the Arkham dVPN network.",
	Long:  `An interactive command-line interface to run an Arkham node and manage your Arkham wallet.`,
	Run:   run,
}

// run is the main entry point for the interactive CLI.
func run(cmd *cobra.Command, args []string) {
	// Load .env file from the current directory.
	if err := godotenv.Load(); err != nil {
		log.Println("Info: .env file not found, using default public RPC endpoint.")
	}

	if heliusApiKey := os.Getenv("HELIUS_API_KEY"); heliusApiKey != "" {
		devnetRpcEndpoint = fmt.Sprintf("https://devnet.helius-rpc.com/?api-key=%s", heliusApiKey)
		log.Println("Info: Using Helius RPC endpoint.")
	}

	myFigure := figure.NewFigure("ARKHAM", "larry3d", true)
	fmt.Println(titleStyle.Render(myFigure.String()))

	// The main application loop is now wrapped in profile selection.
	for {
		signer, profileName, err := runProfileSelection()
		if err != nil {
			// This error is returned when the user chooses to exit.
			fmt.Println("Exiting Arkham CLI.")
			os.Exit(0)
		}
		runInteractive(signer, profileName)
	}
}

// runProfileSelection handles the UI for choosing or creating a wallet profile.
func runProfileSelection() (solana.PrivateKey, string, error) {
	db, err := storage.NewWalletStorage()
	if err != nil {
		panic(fmt.Sprintf("failed to connect to wallet storage: %v", err))
	}

	// If no warden wallet exists, run the first-time initialization.
	if !isInitialized(db) {
		runInit(db)
	}

	for {
		profiles, err := db.GetAllWalletNames()
		if err != nil {
			panic(fmt.Sprintf("failed to get wallet profiles: %v", err))
		}

		options := append(profiles, "Create New Seeker Profile", "Exit")

		selection := ""
		prompt := &survey.Select{
			Message: promptStyle.Render("Choose a profile to continue:"),
			Options: options,
		}
		survey.AskOne(prompt, &selection)

		switch selection {
		case "Create New Seeker Profile":
			handleCreateSeekerProfile(db)
			// Loop again to show the new profile in the list.
			continue
		case "Exit":
			return nil, "", fmt.Errorf("user exited")
		default: // A profile was selected
			signer, err := db.GetWallet(selection)
			if err != nil {
				panic(fmt.Sprintf("failed to get wallet for profile '%s': %v", selection, err))
			}
			return signer, selection, nil
		}
	}
}

func runInteractive(signer solana.PrivateKey, profileName string) {
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}

	fmt.Printf("\n---\n")
	fmt.Println(titleStyle.Render(fmt.Sprintf("Operating with profile: %s", profileName)))
	fmt.Println(promptStyle.Render(fmt.Sprintf("Address: %s", signer.PublicKey())))
	fmt.Printf("---\n\n")

	var menuOptions []string
	if profileName == "warden" {
		fmt.Println(promptStyle.Render("Checking registration status..."))
		isRegistered, err := client.IsWardenRegistered()
		if err != nil {
			fmt.Println(warningStyle.Render(fmt.Sprintf("Could not check warden status: %v", err)))
			return
		}
		if isRegistered {
			menuOptions = []string{
				"View Warden Dashboard",
				"Test Submit Bandwidth Proof",
				"Wallet Management",
				"Switch Profile",
			}
		} else {
			menuOptions = []string{
				"Register as Warden",
				"Wallet Management",
				"Switch Profile",
			}
		}
	} else if profileName == "seeker" {
		menuOptions = []string{
			"View Seeker Dashboard",
			"Deposit Escrow",
			"Start Connection",
			"Generate Signature for Proof",
			"Wallet Management",
			"Switch Profile",
		}
	}

	menu := &survey.Select{
		Message: promptStyle.Render("Choose an action:"),
		Options: menuOptions,
		Help:    "Use the arrow keys to navigate, and press Enter to select.",
	}

	var choice string
	err = survey.AskOne(menu, &choice)
	if err != nil {
		fmt.Println(warningStyle.Render(err.Error()))
		return
	}

	switch choice {
	// Warden actions
	case "Register as Warden":
		handleRegistration(signer)
	case "View Warden Dashboard":
		fmt.Println(titleStyle.Render("\n📊 Warden Dashboard (Coming Soon)"))
	case "Test Submit Bandwidth Proof":
		handleBandwidthProof(signer)
	// Seeker actions
	case "View Seeker Dashboard":
		fmt.Println(titleStyle.Render("\n📊 Seeker Dashboard (Coming Soon)"))
	case "Deposit Escrow":
		handleDepositEscrow(signer)
	case "Start Connection":
		handleStartConnection(signer)
	case "Generate Signature for Proof":
		handleGenerateSignature(signer)
	// Common actions
	case "Wallet Management":
		handleWalletManagement(signer)
	case "Switch Profile":
		return // Exit this interactive loop to go back to profile selection
	}
	fmt.Println()
}

func handleGenerateSignature(signer solana.PrivateKey) {
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}

	wardenPubkeyStr := ""
	wardenPrompt := &survey.Input{Message: "Enter the Warden's public key for the connection:"}
	survey.AskOne(wardenPrompt, &wardenPubkeyStr, survey.WithValidator(survey.Required))

	wardenPubkey, err := solana.PublicKeyFromBase58(wardenPubkeyStr)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid Warden public key."))
		return
	}

	mbConsumedStr := "10"
	mbPrompt := &survey.Input{Message: "Enter MB consumed:", Default: "10"}
	survey.AskOne(mbPrompt, &mbConsumedStr)
	mbConsumed, _ := strconv.ParseUint(mbConsumedStr, 10, 64)

	// The timestamp is critical and must be shared with the warden.
	timestamp := time.Now().Unix()

	fmt.Println(promptStyle.Render("\nGenerating Seeker signature..."))
	signature, err := client.GenerateBandwidthProofSignature(wardenPubkey, mbConsumed, timestamp)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n❌ Failed to generate signature: %v", err)))
		return
	}

	fmt.Println(titleStyle.Render("\n✅ Signature Generated!"))
	fmt.Println(promptStyle.Render("   Provide these details to the Warden:"))
	fmt.Println(infoStyle.Render(fmt.Sprintf("   Timestamp: %d", timestamp)))
	fmt.Println(infoStyle.Render(fmt.Sprintf("   Signature: %s", hex.EncodeToString(signature[:]))))
}

func handleBandwidthProof(signer solana.PrivateKey) {
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}

	seekerPubkeyStr := ""
	seekerPrompt := &survey.Input{Message: "Enter the Seeker's public key:"}
	survey.AskOne(seekerPrompt, &seekerPubkeyStr, survey.WithValidator(survey.Required))
	seekerPubkey, err := solana.PublicKeyFromBase58(seekerPubkeyStr)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid Seeker public key."))
		return
	}

	mbConsumedStr := "10"
	mbPrompt := &survey.Input{Message: "Enter MB consumed:", Default: "10"}
	survey.AskOne(mbPrompt, &mbConsumedStr)
	mbConsumed, _ := strconv.ParseUint(mbConsumedStr, 10, 64)

	timestampStr := ""
	tsPrompt := &survey.Input{Message: "Enter the Timestamp from the Seeker:"}
	survey.AskOne(tsPrompt, &timestampStr, survey.WithValidator(survey.Required))
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid timestamp."))
		return
	}

	sigStr := ""
	sigPrompt := &survey.Input{Message: "Enter the Seeker's Signature (hex):"}
	survey.AskOne(sigPrompt, &sigStr, survey.WithValidator(survey.Required))
	seekerSigBytes, err := hex.DecodeString(sigStr)
	if err != nil || len(seekerSigBytes) != 64 {
		fmt.Println(warningStyle.Render("Invalid signature format."))
		return
	}
	var seekerSig solana.Signature
	copy(seekerSig[:], seekerSigBytes)

	fmt.Println(promptStyle.Render(fmt.Sprintf("\nSubmitting bandwidth proof for %d MB...", mbConsumed)))
	sig, err := client.SubmitBandwidthProof(mbConsumed, seekerPubkey, seekerSig, timestamp)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n❌ Bandwidth proof submission failed: %v", err)))
		return
	}

	fmt.Println(titleStyle.Render("\n✅ Bandwidth Proof Submitted Successfully!"))
	fmt.Printf("   Transaction Signature: %s\n", sig.String())
}

func handleCreateSeekerProfile(db *storage.WalletStorage) {
	fmt.Println(promptStyle.Render("\nCreating new Seeker wallet..."))
	newWallet := solana.NewWallet()
	err := db.SaveWallet("seeker", newWallet.PrivateKey)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("❌ Failed to save new seeker wallet: %v", err)))
		return
	}
	fmt.Println(titleStyle.Render("\n✅ Seeker Profile Created!"))
	fmt.Println(promptStyle.Render("   Your seeker wallet address:"), newWallet.PublicKey().String())
	fmt.Println(promptStyle.Render("\nPress Enter to continue..."))
	fmt.Scanln()
}

func handleDepositEscrow(signer solana.PrivateKey) {
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}

	amountStr := ""
	amountPrompt := &survey.Input{Message: "Enter amount of SOL to deposit into escrow:"}
	survey.AskOne(amountPrompt, &amountStr, survey.WithValidator(survey.Required))

	amountFloat, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid amount entered."))
		return
	}
	amountLamports := uint64(amountFloat * float64(solana.LAMPORTS_PER_SOL))

	fmt.Println(promptStyle.Render(fmt.Sprintf("\nDepositing %f SOL into escrow...", amountFloat)))
	sig, err := client.DepositEscrow(amountLamports)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n❌ Escrow deposit failed: %v", err)))
		return
	}

	fmt.Println(titleStyle.Render("\n✅ Escrow Deposit Successful!"))
	fmt.Printf("   Transaction Signature: %s\n", sig.String())
}

func handleStartConnection(signer solana.PrivateKey) {
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}

	wardenPubkeyStr := ""
	wardenPrompt := &survey.Input{Message: "Enter the Warden's public key to connect to:"}
	survey.AskOne(wardenPrompt, &wardenPubkeyStr, survey.WithValidator(survey.Required))

	wardenPubkey, err := solana.PublicKeyFromBase58(wardenPubkeyStr)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid Warden public key."))
		return
	}

	// For now, we use a default estimated usage.
	estimatedMb := uint64(100)

	fmt.Println(promptStyle.Render(fmt.Sprintf("\nStarting connection with Warden %s...", wardenPubkeyStr)))
	sig, err := client.StartConnection(wardenPubkey, estimatedMb)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n❌ Failed to start connection: %v", err)))
		return
	}

	fmt.Println(titleStyle.Render("\n✅ Connection Started Successfully!"))
	fmt.Printf("   This created the on-chain Connection account.\n")
	fmt.Printf("   Transaction Signature: %s\n", sig.String())
}

func runInit(db *storage.WalletStorage) {
	fmt.Println(titleStyle.Render("🚀 Welcome to Arkham! Let's get you set up."))
	fmt.Println(promptStyle.Render("   Creating new default 'warden' wallet..."))
	newWallet := solana.NewWallet()
	err := db.SaveWallet("warden", newWallet.PrivateKey)
	if err != nil {
		panic(fmt.Sprintf("❌ Failed to save new warden wallet: %v", err))
	}
	fmt.Println(titleStyle.Render("\n✅ Initialization Complete!"))
	fmt.Println(promptStyle.Render("   Your warden wallet address:"), newWallet.PublicKey().String())
	fmt.Println(promptStyle.Render("\nPress Enter to continue..."))
	fmt.Scanln()
}

func handleWalletManagement(signer solana.PrivateKey) {
	fmt.Println()
	menu := &survey.Select{
		Message: promptStyle.Render("Wallet Management:"),
		Options: []string{"View Address", "View Balance", "Send SOL", "Export Wallet (UNSAFE)", "Back to Main Menu"},
	}
	var choice string
	survey.AskOne(menu, &choice)

	switch choice {
	case "View Address":
		viewAddress(signer)
	case "View Balance":
		viewBalance(signer)
	case "Send SOL":
		sendSol(signer)
	case "Export Wallet (UNSAFE)":
		exportWallet(signer)
	case "Back to Main Menu":
		return
	}
}

func viewAddress(signer solana.PrivateKey) {
	fmt.Println(titleStyle.Render("\n🔑 Your Current Wallet Address:"))
	fmt.Println(signer.PublicKey().String())
}

func viewBalance(signer solana.PrivateKey) {
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}
	fmt.Println(promptStyle.Render("\nChecking balance... Please wait."))
	balanceLamports, err := client.GetBalance(signer.PublicKey())
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n❌ Failed to get balance: %v", err)))
		return
	}
	balanceSOL := float64(balanceLamports) / float64(solana.LAMPORTS_PER_SOL)
	fmt.Println(titleStyle.Render("\n💰 Your Wallet Balance:"))
	fmt.Printf("   %.9f SOL\n", balanceSOL)
}

func exportWallet(signer solana.PrivateKey) {
	fmt.Println(warningStyle.Render("\n⚠️ WARNING: EXPORTING YOUR PRIVATE KEY ⚠️"))
	fmt.Println(promptStyle.Render("Sharing your private key can result in the permanent loss of your funds."))
	confirm := false
	prompt := &survey.Confirm{Message: "Are you absolutely sure?", Default: false}
	survey.AskOne(prompt, &confirm)
	if !confirm {
		fmt.Println(promptStyle.Render("\nExport cancelled."))
		return
	}
	fmt.Println(titleStyle.Render("\n🔐 Your Private Key (Base58):"))
	fmt.Println(signer.String())
}

func sendSol(signer solana.PrivateKey) {
	fmt.Println(promptStyle.Render("\n💸 Send SOL"))
	recipientStr := ""
	addrPrompt := &survey.Input{Message: "Enter recipient address:"}
	survey.AskOne(addrPrompt, &recipientStr, survey.WithValidator(survey.Required))
	recipient, err := solana.PublicKeyFromBase58(recipientStr)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid recipient address."))
		return
	}
	amountStr := ""
	amountPrompt := &survey.Input{Message: "Enter amount of SOL to send:"}
	survey.AskOne(amountPrompt, &amountStr, survey.WithValidator(survey.Required))
	amountFloat, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid amount entered."))
		return
	}
	amountLamports := uint64(amountFloat * float64(solana.LAMPORTS_PER_SOL))
	confirm := false
	confirmPrompt := &survey.Confirm{
		Message: fmt.Sprintf("You are about to send %f SOL to %s. Continue?", amountFloat, recipient.String()),
		Default: false,
	}
	survey.AskOne(confirmPrompt, &confirm)
	if !confirm {
		fmt.Println(promptStyle.Render("\nSend cancelled."))
		return
	}
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}
	fmt.Println(promptStyle.Render("\nSending transaction... Please wait."))
	sig, err := client.SendSol(recipient, amountLamports)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n❌ Failed to send SOL: %v", err)))
		return
	}
	fmt.Println(titleStyle.Render("\n✅ Transaction Sent Successfully!"))
	fmt.Printf("   Transaction Signature: %s\n", sig.String())
}

func handleRegistration(signer solana.PrivateKey) {
	fmt.Println(promptStyle.Render("\n🚀 Warden Registration"))
	// ... (rest of the function needs to be updated to accept signer)
	stakeTokenStr := ""
	tokenPrompt := &survey.Select{
		Message: "Choose your stake token:",
		Options: []string{"SOL", "USDC"},
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
	stakeAmountStr := ""
	amountPrompt := &survey.Input{
		Message: fmt.Sprintf("Enter amount of %s to stake:", stakeTokenStr),
	}
	survey.AskOne(amountPrompt, &stakeAmountStr, survey.WithValidator(survey.Required))
	stakeAmountFloat, err := strconv.ParseFloat(stakeAmountStr, 64)
	if err != nil {
		fmt.Println(warningStyle.Render("Invalid amount entered."))
		return
	}
	var amountLamports uint64
	if stakeToken == arkham_protocol.StakeToken_Sol {
		amountLamports = uint64(stakeAmountFloat * float64(solana.LAMPORTS_PER_SOL))
	} else {
		amountLamports = uint64(stakeAmountFloat * 1_000_000)
	}
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		return
	}
	peerID := "12D3KooWPlaceholderPeerID" + signer.PublicKey().String()[:10]
	regionCode := uint8(0)
	ipHash := sha256.Sum256([]byte("127.0.0.1"))
	fmt.Println(promptStyle.Render(fmt.Sprintf("\nRegistering as Warden with %f %s...", stakeAmountFloat, stakeTokenStr)))
	fmt.Println(promptStyle.Render("Please wait..."))
	sig, err := client.InitializeWarden(
		stakeToken,
		amountLamports,
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
}

func isInitialized(db *storage.WalletStorage) bool {
	_, err := db.GetWallet("warden")
	return err == nil
}

func openURL(url string) {
	fmt.Println(promptStyle.Render(fmt.Sprintf("Opening %s in your browser...", url)))
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Error opening URL: %v", err)))
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
