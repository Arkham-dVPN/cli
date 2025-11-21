package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"crypto/sha256"

	"arkham-cli/cmd"
	arkham_protocol "arkham-cli/solana"
	"arkham-cli/storage"
	"github.com/gagliardetto/solana-go"
)

//go:embed all:gui-assets
var embeddedUI embed.FS

func main() {
	// Special handling for the 'gui' command before Cobra takes over.
	if len(os.Args) > 1 && os.Args[1] == "gui" {
		startGuiServer()
	} else {
		cmd.Execute()
	}
}

// --- API Handlers ---

func handleGetHistory(w http.ResponseWriter, r *http.Request) {
	profileName := r.URL.Query().Get("profile")
	if profileName == "" {
		http.Error(w, "Missing 'profile' query parameter", http.StatusBadRequest)
		return
	}

	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "Failed to open wallet storage", http.StatusInternalServerError)
		return
	}
	signer, err := db.GetWallet(profileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Profile '%s' not found", profileName), http.StatusBadRequest)
		return
	}

	client, err := arkham_protocol.NewClient(cmd.GetRpcEndpoint(), signer)
	if err != nil {
		http.Error(w, "Failed to create solana client", http.StatusInternalServerError)
		return
	}

	history, err := client.GetHistory(signer.PublicKey())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get transaction history: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func handleGetProfiles(w http.ResponseWriter, r *http.Request) {
	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "failed to connect to wallet storage", http.StatusInternalServerError)
		return
	}
	profiles, err := db.GetAllWalletNames()
	if err != nil {
		http.Error(w, "failed to get wallet profiles", http.StatusInternalServerError)
		return
	}
	if profiles == nil {
		profiles = []string{} // Return empty array instead of null
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

func handleGetAddresses(w http.ResponseWriter, r *http.Request) {
	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "failed to connect to wallet storage", http.StatusInternalServerError)
		return
	}
	wallets, err := db.GetAllWallets()
	if err != nil {
		http.Error(w, "failed to get wallets", http.StatusInternalServerError)
		return
	}

	addresses := make(map[string]string)
	for name, key := range wallets {
		addresses[name] = key.PublicKey().String()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(addresses)
}

type CreateProfileRequest struct {
	Profile string `json:"profile"`
}

func handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "Failed to open wallet storage", http.StatusInternalServerError)
		return
	}

	newWallet := solana.NewWallet()
	err = db.SaveWallet(req.Profile, newWallet.PrivateKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save new %s wallet: %v", req.Profile, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"profile": req.Profile,
		"publicKey": newWallet.PublicKey().String(),
	})
}

func handleGetBalance(w http.ResponseWriter, r *http.Request) {
	profileName := r.URL.Query().Get("profile")
	if profileName == "" {
		http.Error(w, "Missing 'profile' query parameter", http.StatusBadRequest)
		return
	}

	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "Failed to open wallet storage", http.StatusInternalServerError)
		return
	}
	signer, err := db.GetWallet(profileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Profile '%s' not found", profileName), http.StatusBadRequest)
		return
	}

	client, err := arkham_protocol.NewClient(cmd.GetRpcEndpoint(), signer)
	if err != nil {
		http.Error(w, "Failed to create solana client", http.StatusInternalServerError)
		return
	}

	balance, err := client.GetBalance(signer.PublicKey())
	if err != nil {
		http.Error(w, "Failed to get balance", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]uint64{
		"lamports": balance,
	})
}

func handleGetTokenBalance(w http.ResponseWriter, r *http.Request) {
	profileName := r.URL.Query().Get("profile")
	if profileName == "" {
		http.Error(w, "Missing 'profile' query parameter", http.StatusBadRequest)
		return
	}
	mintAddress := r.URL.Query().Get("mint")
	if mintAddress == "" {
		http.Error(w, "Missing 'mint' query parameter", http.StatusBadRequest)
		return
	}
	mint, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		http.Error(w, "Invalid 'mint' query parameter", http.StatusBadRequest)
		return
	}

	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "Failed to open wallet storage", http.StatusInternalServerError)
		return
	}
	signer, err := db.GetWallet(profileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Profile '%s' not found", profileName), http.StatusBadRequest)
		return
	}

	client, err := arkham_protocol.NewClient(cmd.GetRpcEndpoint(), signer)
	if err != nil {
		http.Error(w, "Failed to create solana client", http.StatusInternalServerError)
		return
	}

	balance, err := client.GetTokenBalance(signer.PublicKey(), mint)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get token balance: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]uint64{
		"uiAmount": balance,
	})
}

// WardenView is a custom struct to ensure correct JSON serialization for the frontend.
type WardenView struct {
	Authority             solana.PublicKey       `json:"authority"`
	PeerId                string                 `json:"peerId"`
	StakeToken            map[string]interface{} `json:"stakeToken"`
	StakeAmount           uint64                 `json:"stakeAmount"`
	StakeValueUsd         uint64                 `json:"stakeValueUsd"`
	Tier                  map[string]interface{} `json:"tier"`
	StakedAt              int64                  `json:"stakedAt"`
	UnstakeRequestedAt    *int64                 `json:"unstakeRequestedAt,omitempty"`
	TotalBandwidthServed  uint64                 `json:"totalBandwidthServed"`
	TotalEarnings         uint64                 `json:"totalEarnings"`
	PendingClaims         uint64                 `json:"pendingClaims"`
	ArkhamTokensEarned    uint64                 `json:"arkhamTokensEarned"`
	ReputationScore       uint32                 `json:"reputationScore"`
	SuccessfulConnections uint64                 `json:"successfulConnections"`
	FailedConnections     uint64                 `json:"failedConnections"`
	UptimePercentage      uint16                 `json:"uptimePercentage"`
	LastActive            int64                  `json:"lastActive"`
	RegionCode            uint8                  `json:"regionCode"`
	IpHash                [32]uint8              `json:"ipHash"`
	PremiumPoolRank       *uint16                `json:"premiumPoolRank,omitempty"`
	ActiveConnections     uint8                  `json:"activeConnections"`
}

// Seeker-specific view model for frontend JSON serialization.
type SeekerView struct {
	Authority              solana.PublicKey  `json:"authority"`
	EscrowBalance          uint64            `json:"escrowBalance"`
	PrivateEscrow          *solana.PublicKey `json:"privateEscrow,omitempty"`
	TotalBandwidthConsumed uint64            `json:"totalBandwidthConsumed"`
	TotalSpent             uint64            `json:"totalSpent"`
	ActiveConnections      uint8             `json:"activeConnections"`
	PremiumExpiresAt       *int64            `json:"premiumExpiresAt,omitempty"`
}


func handleWardenStatus(w http.ResponseWriter, r *http.Request) {
	profileName := r.URL.Query().Get("profile")
	if profileName == "" {
		http.Error(w, "Missing 'profile' query parameter", http.StatusBadRequest)
		return
	}

	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "Failed to open wallet storage", http.StatusInternalServerError)
		return
	}
	signer, err := db.GetWallet(profileName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"is_registered": false, "warden": nil})
		return
	}

	client, err := arkham_protocol.NewClient(cmd.GetRpcEndpoint(), signer)
	if err != nil {
		http.Error(w, "Failed to create solana client", http.StatusInternalServerError)
		return
	}

	type WardenStatusResponse struct {
		IsRegistered bool        `json:"is_registered"`
		Warden       *WardenView `json:"warden"`
	}

	isRegistered, err := client.IsWardenRegistered()
	if err != nil || !isRegistered {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WardenStatusResponse{IsRegistered: false, Warden: nil})
		return
	}

	wardenAccount, err := client.FetchWardenAccount()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WardenStatusResponse{IsRegistered: false, Warden: nil})
		return
	}

	// Manually create the view model to match frontend expectations for enums.
	wardenView := &WardenView{
		Authority:             wardenAccount.Authority,
		PeerId:                wardenAccount.PeerId,
		StakeAmount:           wardenAccount.StakeAmount,
		StakeValueUsd:         wardenAccount.StakeValueUsd,
		StakedAt:              wardenAccount.StakedAt,
		UnstakeRequestedAt:    wardenAccount.UnstakeRequestedAt,
		TotalBandwidthServed:  wardenAccount.TotalBandwidthServed,
		TotalEarnings:         wardenAccount.TotalEarnings,
		PendingClaims:         wardenAccount.PendingClaims,
		ArkhamTokensEarned:    wardenAccount.ArkhamTokensEarned,
		ReputationScore:       wardenAccount.ReputationScore,
		SuccessfulConnections: wardenAccount.SuccessfulConnections,
		FailedConnections:     wardenAccount.FailedConnections,
		UptimePercentage:      wardenAccount.UptimePercentage,
		LastActive:            wardenAccount.LastActive,
		RegionCode:            wardenAccount.RegionCode,
		IpHash:                wardenAccount.IpHash,
		PremiumPoolRank:       wardenAccount.PremiumPoolRank,
		ActiveConnections:     wardenAccount.ActiveConnections,
		StakeToken:            map[string]interface{}{strings.Title(wardenAccount.StakeToken.String()): make(map[string]interface{})},
		Tier:                  map[string]interface{}{strings.Title(wardenAccount.Tier.String()): make(map[string]interface{})},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WardenStatusResponse{IsRegistered: true, Warden: wardenView})
}

func handleSeekerStatus(w http.ResponseWriter, r *http.Request) {
	profileName := r.URL.Query().Get("profile")
	if profileName == "" {
		http.Error(w, "Missing 'profile' query parameter", http.StatusBadRequest)
		return
	}

	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "Failed to open wallet storage", http.StatusInternalServerError)
		return
	}
	signer, err := db.GetWallet(profileName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"is_registered": false, "seeker": nil})
		return
	}

	client, err := arkham_protocol.NewClient(cmd.GetRpcEndpoint(), signer)
	if err != nil {
		http.Error(w, "Failed to create solana client", http.StatusInternalServerError)
		return
	}

	type SeekerStatusResponse struct {
		IsRegistered bool        `json:"is_registered"`
		Seeker       *SeekerView `json:"seeker"`
	}

	isRegistered, err := client.IsSeekerRegistered()
	if err != nil || !isRegistered {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SeekerStatusResponse{IsRegistered: false, Seeker: nil})
		return
	}

	seekerAccount, err := client.FetchSeekerAccount()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SeekerStatusResponse{IsRegistered: false, Seeker: nil})
		return
	}

	seekerView := &SeekerView{
		Authority:              seekerAccount.Authority,
		EscrowBalance:          seekerAccount.EscrowBalance,
		PrivateEscrow:          seekerAccount.PrivateEscrow,
		TotalBandwidthConsumed: seekerAccount.TotalBandwidthConsumed,
		TotalSpent:             seekerAccount.TotalSpent,
		ActiveConnections:      seekerAccount.ActiveConnections,
		PremiumExpiresAt:       seekerAccount.PremiumExpiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SeekerStatusResponse{IsRegistered: true, Seeker: seekerView})
}

// A helper function to fetch the current SOL price from CoinGecko.
func getSolPrice() (float64, error) {
	resp, err := http.Get("https://api.coingecko.com/api/v3/simple/price?ids=solana&vs_currencies=usd")
	if err != nil {
		return 0, fmt.Errorf("failed to call coingecko: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("coingecko returned non-200 status: %s", resp.Status)
	}

	var priceData struct {
		Solana struct {
			Usd float64 `json:"usd"`
		} `json:"solana"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&priceData); err != nil {
		return 0, fmt.Errorf("failed to decode coingecko response: %w", err)
	}

	if priceData.Solana.Usd == 0 {
		return 0, fmt.Errorf("did not receive a valid price from coingecko")
	}

	return priceData.Solana.Usd, nil
}

// WardenApiView is the simplified warden model for the frontend.
type WardenApiView struct {
	ID         string  `json:"id"`
	Nickname   string  `json:"nickname"`
	Location   string  `json:"location"`
	Reputation float64 `json:"reputation"`
	PricePerGb float64 `json:"price"` // Price in USD per GB
}

func handleGetWardens(w http.ResponseWriter, r *http.Request) {
	client, err := arkham_protocol.NewReadOnlyClient(cmd.GetRpcEndpoint())
	if err != nil {
		http.Error(w, "Failed to create solana client", http.StatusInternalServerError)
		return
	}

	// Fetch all required data concurrently
	var protocolConfig *arkham_protocol.ProtocolConfig
	var wardens []*arkham_protocol.Warden
	var solPrice float64
	var configErr, wardensErr, priceErr error

	ch := make(chan func(), 3)

	go func() {
		protocolConfig, configErr = client.FetchProtocolConfig()
		ch <- func() {}
	}()
	go func() {
		wardens, wardensErr = client.FetchAllWardens()
		ch <- func() {}
	}()
	go func() {
		solPrice, priceErr = getSolPrice()
		ch <- func() {}
	}()

	for i := 0; i < 3; i++ {
		(<-ch)()
	}

	if configErr != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch protocol config: %v", configErr), http.StatusInternalServerError)
		return
	}
	if wardensErr != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch wardens: %v", wardensErr), http.StatusInternalServerError)
		return
	}
	if priceErr != nil {
		// Don't fail the whole request if price is unavailable, just default to 0
		log.Printf("Warning: could not fetch SOL price: %v", priceErr)
		solPrice = 0
	}

	// Create lookup maps for easier access
	geoPremiumMap := make(map[uint8]uint16)
	for _, p := range protocolConfig.GeoPremiums {
		geoPremiumMap[p.RegionCode] = p.PremiumBps
	}
	tierMultiplierMap := map[arkham_protocol.Tier]uint16{
		arkham_protocol.Tier_Bronze: protocolConfig.TierMultipliers[0],
		arkham_protocol.Tier_Silver: protocolConfig.TierMultipliers[1],
		arkham_protocol.Tier_Gold:   protocolConfig.TierMultipliers[2],
	}
	regionMap := map[uint8]string{
		0: "🇺🇸 USA",
		1: "🇪🇺 Europe",
		2: "🇯🇵 Asia",
	}

	// Process wardens into the API view
	response := make([]*WardenApiView, 0)
	for _, warden := range wardens {
		geoPremiumBps := float64(geoPremiumMap[warden.RegionCode])
		tierMultiplierBps := float64(tierMultiplierMap[warden.Tier])

		// Calculate effective rate per MB in lamports
		effectiveRatePerMb := float64(protocolConfig.BaseRatePerMb) *
			(1 + geoPremiumBps/10000.0) *
			(tierMultiplierBps / 10000.0)

		// Convert to price per GB in USD
		pricePerGbLamports := effectiveRatePerMb * 1024
		pricePerGbSol := pricePerGbLamports / float64(solana.LAMPORTS_PER_SOL)
		pricePerGbUsd := pricePerGbSol * solPrice

		apiView := &WardenApiView{
			ID:         warden.Authority.String(),
			Nickname:   warden.Authority.String()[:6] + "..." + warden.Authority.String()[len(warden.Authority.String())-4:],
			Location:   regionMap[warden.RegionCode],
			Reputation: float64(warden.ReputationScore) / 2000.0, // 0-10000 to 0-5
			PricePerGb: pricePerGbUsd,
		}
		response = append(response, apiView)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}


type RegisterWardenRequest struct {
	Profile     string  `json:"profile"`
	StakeToken  string  `json:"stakeToken"`
	StakeAmount float64 `json:"stakeAmount"`
}

func handleRegisterWarden(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterWardenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	db, err := storage.NewWalletStorage()
	if err != nil {
		http.Error(w, "Failed to open wallet storage", http.StatusInternalServerError)
		return
	}
	signer, err := db.GetWallet(req.Profile)
	if err != nil {
		http.Error(w, fmt.Sprintf("Profile '%s' not found", req.Profile), http.StatusBadRequest)
		return
	}

	client, err := arkham_protocol.NewClient(cmd.GetRpcEndpoint(), signer)
	if err != nil {
		http.Error(w, "Failed to create solana client", http.StatusInternalServerError)
		return
	}

	var stakeTokenEnum arkham_protocol.StakeToken
	if req.StakeToken == "SOL" {
		stakeTokenEnum = arkham_protocol.StakeToken_Sol
	} else if req.StakeToken == "USDC" {
		stakeTokenEnum = arkham_protocol.StakeToken_Usdc
	} else {
		http.Error(w, "Invalid stake token specified", http.StatusBadRequest)
		return
	}

	var amountLamports uint64
	if stakeTokenEnum == arkham_protocol.StakeToken_Sol {
		amountLamports = uint64(req.StakeAmount * float64(solana.LAMPORTS_PER_SOL))
	} else {
		amountLamports = uint64(req.StakeAmount * 1_000_000)
	}
	
	peerID := "12D3KooWPlaceholderPeerID" + signer.PublicKey().String()[:10]
	regionCode := uint8(0)
	ipHash := sha256.Sum256([]byte("127.0.0.1"))

	sig, err := client.InitializeWarden(stakeTokenEnum, amountLamports, peerID, regionCode, ipHash)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send registration transaction: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"transactionSignature": sig.String(),
	})
}


// --- GUI Server ---

func findNextAvailablePort(startPort int) (string, error) {
	// Try up to 100 ports starting from startPort
	for port := startPort; port < startPort+100; port++ {
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			// Port is available, close the listener and return the port
			listener.Close()
			return strconv.Itoa(port), nil
		}
	}
	// If we get here, we couldn't find a port in the range
	return "", fmt.Errorf("could not find an available port between %d and %d", startPort, startPort+99)
}

func startGuiServer() {
	cmd.GetRpcEndpoint()

	content, err := fs.Sub(embeddedUI, "gui-assets")
	if err != nil {
		log.Fatalf("Failed to get embedded subdirectory: %v", err)
	}

	// API Endpoints
	http.HandleFunc("/api/profiles", handleGetProfiles)
	http.HandleFunc("/api/addresses", handleGetAddresses)
	http.HandleFunc("/api/create-profile", handleCreateProfile)
	http.HandleFunc("/api/register-warden", handleRegisterWarden)
	http.HandleFunc("/api/balance", handleGetBalance)
	http.HandleFunc("/api/token-balance", handleGetTokenBalance)
	http.HandleFunc("/api/warden-status", handleWardenStatus)
	http.HandleFunc("/api/seeker-status", handleSeekerStatus)
	http.HandleFunc("/api/wardens", handleGetWardens)
	http.HandleFunc("/api/history", handleGetHistory)

	// Frontend File Server
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "index.html"
		} else {
			path = path[1:]
		}

		file, err := content.Open(path)
		if err != nil {
			index, err := content.Open("index.html")
			if err != nil {
				http.Error(w, "index.html not found", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			stat, _ := index.Stat()
			http.ServeContent(w, r, "index.html", stat.ModTime(), index.(io.ReadSeeker))
			return
		}
		defer file.Close()

		stat, _ := file.Stat()
		http.ServeContent(w, r, r.URL.Path, stat.ModTime(), file.(io.ReadSeeker))
	})

	port, err := findNextAvailablePort(8088)
	if err != nil {
		log.Fatalf("Failed to start GUI server: %v", err)
	}

	url := fmt.Sprintf("http://localhost:%s", port)
	fmt.Printf("🚀 Launching Arkham GUI at %s\n", url)

	go func() {
		var err error
		switch runtime.GOOS {
		case "linux":
			err = exec.Command("xdg-open", url).Start()
		case "darwin":
			err = exec.Command("open", url).Start()
		case "windows":
			err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		default:
			err = fmt.Errorf("unsupported platform")
		}
		if err != nil {
			log.Printf("Failed to open browser: %v\n", err)
		}
	}()

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
