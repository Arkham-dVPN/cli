package cmd

import "github.com/charmbracelet/lipgloss"

// Styles used across the CLI commands
var (
	titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFA500")). // Gold/Amber
		Bold(true).
		Padding(1, 0)

	promptStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CCCCCC")) // Light Gray

	warningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF6347")). // Tomato red
		Bold(true)
)
