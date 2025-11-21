package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
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
	http.HandleFunc("/api/warden-status", handleWardenStatus)
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

	port := "8088"
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
