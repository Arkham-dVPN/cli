package cmd

import (
	"arkham-cli/storage"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"

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

	if !isInitialized() {
		runInit()
	}
	runInteractive()
}

func runInteractive() {
	// Create a client to interact with the blockchain
	signer := mustGetWallet()
	client, err := arkham_protocol.NewClient(devnetRpcEndpoint, signer)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Failed to create Solana client: %v", err)))
		os.Exit(1)
	}

	// Check if the user is already registered as a warden
	fmt.Println(promptStyle.Render("Checking registration status..."))
	isRegistered, err := client.IsWardenRegistered()
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Could not check warden status: %v", err)))
		os.Exit(1)
	}

	var menuOptions []string
	if isRegistered {
		menuOptions = []string{
			"View Warden Dashboard",
			"Wallet Management",
			"Exit",
		}
	} else {
		menuOptions = []string{
			"Register as Warden",
			"Wallet Management",
			"Exit",
		}
	}

	menu := &survey.Select{
		Message: promptStyle.Render("Choose an action:"),
		Options: menuOptions,
		Help:    "Use the arrow keys to navigate, and press Enter to select.",
	}

	for {
		var choice string
		err := survey.AskOne(menu, &choice)
		if err != nil {
			fmt.Println(warningStyle.Render(err.Error()))
			return
		}

		switch choice {
		case "Register as Warden":
			handleRegistration()
			// After successful registration, we should update the state
			// For simplicity, we'll just inform the user to restart.
			fmt.Println(titleStyle.Render("\nPlease restart the CLI to see the Warden Dashboard."))
			os.Exit(0)
		case "View Warden Dashboard":
			// TODO: Implement the warden dashboard
			fmt.Println(titleStyle.Render("\n📊 Warden Dashboard (Coming Soon)"))
			fmt.Println(promptStyle.Render("This will show your tier, reputation, earnings, and other stats."))
		case "Wallet Management":
			handleWalletManagement()
		case "Exit":
			fmt.Println("Exiting Arkham CLI.")
			os.Exit(0)
		}
		fmt.Println()
	}
}

func runInit() {
	fmt.Println(titleStyle.Render("🚀 Welcome to Arkham! Let's get you set up."))
	db, err := storage.Connect()
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("❌ Failed to connect to database: %v", err)))
		os.Exit(1)
	}
	defer db.Close()

	fmt.Println(promptStyle.Render("   Creating new Solana wallet..."))
	newWallet := solana.NewWallet()

	err = db.SaveWallet(newWallet.PrivateKey)
	if err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("❌ Failed to save new wallet: %v", err)))
		os.Exit(1)
	}

	fmt.Println(titleStyle.Render("\n✅ Initialization Complete!"))
	fmt.Println(promptStyle.Render("   A new wallet has been created and stored securely in the local database."))
	fmt.Println(promptStyle.Render("   Your wallet address:"), newWallet.PublicKey().String())
	fmt.Println(promptStyle.Render("\nPress Enter to continue to the main menu..."))
	fmt.Scanln()
}

func handleWalletManagement() {
	fmt.Println()
	menu := &survey.Select{
		Message: promptStyle.Render("Wallet Management:"),
		Options: []string{
			"View Address",
			"View Balance",
			"View Transaction History",
			"Send SOL",
			"Export Wallet (UNSAFE)",
			"Back to Main Menu",
		},
	}
	var choice string
	err := survey.AskOne(menu, &choice)
	if err != nil {
		fmt.Println(warningStyle.Render(err.Error()))
		return
	}

	switch choice {
	case "View Address":
		viewAddress()
	case "View Balance":
		viewBalance()
	case "View Transaction History":
		viewTxHistory()
	case "Send SOL":
		sendSol()
	case "Export Wallet (UNSAFE)":
		exportWallet()
	case "Back to Main Menu":
		return
	}
}

func viewAddress() {
	privateKey := mustGetWallet()
	fmt.Println(titleStyle.Render("\n🔑 Your Arkham Wallet Address:"))
	fmt.Println(privateKey.PublicKey().String())
}

func viewBalance() {
	signer := mustGetWallet()
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

func viewTxHistory() {
	privateKey := mustGetWallet()
	url := fmt.Sprintf("https://explorer.solana.com/address/%s?cluster=devnet", privateKey.PublicKey().String())
	openURL(url)
}

func exportWallet() {
	fmt.Println(warningStyle.Render("\n⚠️ WARNING: EXPORTING YOUR PRIVATE KEY ⚠️"))
	fmt.Println(promptStyle.Render("Sharing your private key can result in the permanent loss of your funds."))
	confirm := false
	prompt := &survey.Confirm{Message: "Are you absolutely sure?", Default: false}
	survey.AskOne(prompt, &confirm)
	if !confirm {
		fmt.Println(promptStyle.Render("\nExport cancelled."))
		return
	}
	privateKey := mustGetWallet()
	fmt.Println(titleStyle.Render("\n🔐 Your Private Key (Base58):"))
	fmt.Println(privateKey.String())
}

func sendSol() {
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
	signer := mustGetWallet()
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

func handleRegistration() {
	fmt.Println(promptStyle.Render("\n🚀 Warden Registration"))
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
	signer := mustGetWallet()
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

func isInitialized() bool {
	db, err := storage.Connect()
	if err != nil {
		return false
	}
	defer func() {
		// No need to close anything for JSON DB
	}()
	_, err = db.GetWallet()
	return err == nil
}

func mustGetWallet() solana.PrivateKey {
	db, err := storage.Connect()
	if err != nil {
		panic(fmt.Sprintf("failed to connect to database: %v", err))
	}
	defer func() {
		// No need to close anything for JSON DB
	}()
	walletModel, err := db.GetWallet()
	if err != nil {
		panic(fmt.Sprintf("critical: wallet not found or is invalid. If this issue persists, try deleting the wallet file and restarting the application. Error: %v", err))
	}

	// The check for length is now in GetWallet, so we can just return the key.
	// walletModel.PrivateKey is []byte, which is the type of solana.PrivateKey in this library version.
	return walletModel.PrivateKey
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
