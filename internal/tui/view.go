package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Padding(0, 1)

	scopeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true)

	badgeFileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("246")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	badgeVaultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	liveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	modalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("205")).
			Padding(1, 2)
)

func renderView(m Model) string {
	if m.st == stateQuitModal {
		content := "Quit WardenSSH?\n\n" +
			"[k] Kill all sessions & quit (default)\n" +
			"[d] Detach sessions & quit\n" +
			"[c] Cancel"
		return modalBoxStyle.Render(content)
	}

	var sb strings.Builder

	// Header: Title & Scope
	scope := m.hostList.Scope()
	if scope == "" {
		scope = "all"
	}
	sb.WriteString(titleStyle.Render("WardenSSH") + scopeStyle.Render(fmt.Sprintf(" (Scope: %s - Tab to cycle)", scope)) + "\n\n")

	// Filter Line
	sb.WriteString(fmt.Sprintf(" Filter: %s\n\n", m.filter))

	// Host List
	vis := m.hostList.Visible()
	if len(vis) == 0 {
		sb.WriteString("   (no matching hosts)\n")
	} else {
		for i, entry := range vis {
			cur := "  "
			if i == m.cursor {
				cur = cursorStyle.Render("> ")
			}

			liveDot := "  "
			if entry.Live {
				liveDot = liveStyle.Render("● ")
			}

			badge := badgeFileStyle.Render(entry.Source)
			if strings.HasPrefix(entry.Source, "vw:") {
				badge = badgeVaultStyle.Render(entry.Source)
			}

			hostInfo := entry.Alias
			if entry.HostName != "" && entry.HostName != entry.Alias {
				hostInfo += fmt.Sprintf(" (%s)", entry.HostName)
			}

			sb.WriteString(fmt.Sprintf("%s%s%-35s %s\n", cur, liveDot, hostInfo, badge))
		}
	}

	// Footer / Keybindings summary
	sb.WriteString("\n " + scopeStyle.Render("[↑/↓] navigate  [Enter] connect  [Tab] scope  [q] quit"))

	return sb.String()
}
