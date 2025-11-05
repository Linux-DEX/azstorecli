package ui

import "github.com/charmbracelet/lipgloss"

var (
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))

	FooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))
)
