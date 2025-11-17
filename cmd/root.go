package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"arkham-cli/node"

	"github.com/AlecAivazis/survey/v2"
	figure "github.com/common-nighthawk/go-figure"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFA500")). // Gold/Amber
		Bold(true).
		Padding(1, 0)

	promptStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CCCCCC")) // Light Gray

	creditsStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#FFA500")).
		Padding(1, 2)

	authorStyle = lipgloss.NewStyle().Bold(true)
	linkStyle   = lipgloss.NewStyle().Underline(true).Foreground(lipgloss.Color("#00BFFF")) // DeepSkyBlue

	warningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF6347")). // Tomato red
		Bold(true)
)

const (
	githubURL    = "https://github.com/Arkham-dVPN"
	dashboardURL = "https://arkham-dvpn.vercel.app/"
	authorURL    = "https://x.com/davidnzubee"
)

var rootCmd = &cobra.Command{
	Use:   "arkham-cli",
	Short: "Arkham CLI helps you join the Arkham dVPN network.",
	Long: `A command-line interface to run an Arkham node,
turning your machine into a gateway or peer for the decentralized VPN network.`,
	Run: runInteractive,
}

func runInteractive(cmd *cobra.Command, args []string) {
	// 1. Print ASCII art title
	myFigure := figure.NewFigure("ARKHAM", "larry3d", true)
	fmt.Println(titleStyle.Render(myFigure.String()))

	// 2. Define the main menu prompt
	menu := &survey.Select{
		Message: promptStyle.Render("Choose an action:"),
		Options: []string{
			"Register as Warden",
			"Start Gateway Node",
			"Start Peer-Only Node",
			"View Credits",
			"Open Web Dashboard",
			"Open GitHub Repository",
			"Exit",
		},
		Help: "Use the arrow keys to navigate, and press Enter to select.",
	}

	for {
		var choice string
		err := survey.AskOne(menu, &choice)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		// 3. Handle the user's choice
		switch choice {
		case "Register as Warden":
			handleRegistration()
		case "Start Gateway Node":
			startGatewayNode()
		case "Start Peer-Only Node":
			startPeerNode()
		case "View Credits":
			showCredits()
		case "Open Web Dashboard":
			openURL(dashboardURL)
		case "Open GitHub Repository":
			openURL(githubURL)
		case "Exit":
			fmt.Println("Exiting Arkham CLI.")
			os.Exit(0)
		}
		fmt.Println()
	}
}

func startGatewayNode() {
	// Check if running as root/sudo
	if !isRoot() {
		fmt.Println(warningStyle.Render("❌ Gateway Node requires sudo privileges!"))
		fmt.Println(promptStyle.Render("Please run the CLI with sudo:"))
		fmt.Println(promptStyle.Render("  sudo arkham-cli"))
		fmt.Println()
		return
	}

	fmt.Println(promptStyle.Render("Starting Arkham Gateway Node..."))
	fmt.Println(promptStyle.Render("The node will manage network interfaces and start the API server on port 8080."))
	fmt.Println()

	// Start the node directly using the library
	node.StartNode(false) // false = not peer-only, start with API server
}

func startPeerNode() {
	fmt.Println(promptStyle.Render("Starting Arkham Peer-Only Node..."))
	fmt.Println(promptStyle.Render("This node will participate in the network without the API server."))
	fmt.Println()

	// Start the node in peer-only mode
	node.StartNode(true) // true = peer-only mode
}

func showCredits() {
	author := authorStyle.Render("Skipp")
	xLink := linkStyle.Render(authorURL)
	githubLink := linkStyle.Render("https://github.com/DavidNzube101")
	credits := fmt.Sprintf("Project Author: %s\nFollow on X: %s\nCheckout GitHub: %s", author, xLink, githubLink)
	fmt.Println(creditsStyle.Render(credits))
}

func openURL(url string) {
	fmt.Printf("Opening %s in your browser...\n", url)
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
		fmt.Printf("Error opening URL: %v. Please open it manually: %s\n", err, url)
	}
}

// isRoot checks if the program is running with root privileges
func isRoot() bool {
	return os.Geteuid() == 0
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
