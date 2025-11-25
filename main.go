package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"portmanager/process"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	table         table.Model
	textInput     textinput.Model
	ports         []process.PortInfo
	filteredPorts []process.PortInfo
	err           error
	width         int
	height        int
	showModal     bool
	searching     bool
	filter        string
	selectedPort  *process.PortInfo
	statusMessage string
	sortColumn    string
	sortDesc      bool
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		refreshPortsCmd(),
		tickCmd(),
		tea.EnableMouseCellMotion,
	)
}

type portsMsg []process.PortInfo
type errMsg error

func refreshPortsCmd() tea.Cmd {
	return func() tea.Msg {
		ports, err := process.GetListeningPorts()
		if err != nil {
			return errMsg(err)
		}
		return portsMsg(ports)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.showModal {
			switch msg.String() {
			case "y", "Y", "enter":
				if m.selectedPort != nil {
					err := process.KillProcess(m.selectedPort.PID)
					if err != nil {
						m.statusMessage = fmt.Sprintf("Error killing process: %v", err)
					} else {
						m.statusMessage = fmt.Sprintf("Killed process %d", m.selectedPort.PID)
					}
				}
				m.showModal = false
				m.selectedPort = nil
				return m, refreshPortsCmd()
			case "n", "N", "esc":
				m.showModal = false
				m.selectedPort = nil
				m.statusMessage = "Cancelled"
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			}
		} else if m.searching {
			switch msg.String() {
			case "enter":
				m.filter = m.textInput.Value()
				m.searching = false
				m.updateTableRows()
				m.table.GotoTop()
				return m, nil
			case "esc":
				m.searching = false
				m.textInput.SetValue("")
				m.filter = ""
				m.updateTableRows()
				m.table.GotoTop()
				return m, nil
			}
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		} else {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case ":":
				m.searching = true
				m.textInput.Focus()
				m.textInput.SetValue("")
				return m, textinput.Blink
			case "esc":
				if m.filter != "" {
					m.filter = ""
					m.updateTableRows()
					m.table.GotoTop()
					m.statusMessage = "Filter cleared"
				}
				return m, nil
			case "ctrl+d":
				if len(m.filteredPorts) > 0 {
					selectedRow := m.table.Cursor()
					if selectedRow >= 0 && selectedRow < len(m.filteredPorts) {
						m.selectedPort = &m.filteredPorts[selectedRow]
						m.showModal = true
					}
				}
				return m, nil
			case "r":
				return m, refreshPortsCmd()
			}
		}

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
			// Check if click is on header row
			// Base style border is 1 line (Row 0)
			// Header is Row 1
			if msg.Y == 2 {
				m.handleHeaderClick(msg.X)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(msg.Width - 4)
		m.table.SetHeight(msg.Height - 6) // Reduced margin from 10 to 6

	case portsMsg:
		m.ports = msg
		m.updateTableRows()

	case tickMsg:
		if !m.showModal && !m.searching {
			return m, tea.Batch(refreshPortsCmd(), tickCmd())
		}
		return m, tickCmd()

	case errMsg:
		m.err = msg
		return m, nil
	}

	if !m.showModal && !m.searching {
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m *model) handleHeaderClick(x int) {
	// Calculate column ranges
	// Border: 1 char
	// Port: 10
	// Proto: 6
	// PID: 10
	// User: 15
	// Lang: 10
	// Command: 30
	// Note: bubbles/table might add gaps. Assuming no gaps for now or standard gaps.
	// Actually, let's check the columns definition.

	// x is absolute.
	// Left border is at x=0. Content starts at x=1.

	// Simple cumulative width check
	// We need to account for the fact that the table might be centered or styled?
	// No, baseStyle just adds border.

	currentX := 1 // Start after left border

	cols := []struct {
		Name  string
		Width int
	}{
		{"Port", 10},
		{"Proto", 6},
		{"PID", 10},
		{"User", 15},
		{"Lang", 10},
		{"Command", 30},
	}

	clickedCol := ""
	for _, col := range cols {
		// Check if x is within this column
		// We add a bit of buffer for potential padding/gaps if needed,
		// but let's stick to strict width first.
		if x >= currentX && x < currentX+col.Width {
			clickedCol = col.Name
			break
		}
		currentX += col.Width
		// If table adds gap, we might need to increment currentX more.
		// Default gap is usually 1 space? Let's assume 0 for now based on previous output
		// (Output showed tight packing? No, let's look at output again)
		// Output: "│ Port        Proto   PID..."
		// It seems there are spaces. "Port" (4) + spaces.
		// The width 10 includes the spaces.
	}

	if clickedCol != "" {
		if m.sortColumn == clickedCol {
			m.sortDesc = !m.sortDesc
		} else {
			m.sortColumn = clickedCol
			m.sortDesc = false
		}
		m.updateTableRows()
		m.updateHeader()
	}
}

func (m *model) updateHeader() {
	columns := []table.Column{
		{Title: "Port", Width: 10},
		{Title: "Proto", Width: 6},
		{Title: "PID", Width: 10},
		{Title: "User", Width: 15},
		{Title: "Lang", Width: 10},
		{Title: "Command", Width: 30},
	}

	for i, col := range columns {
		if col.Title == m.sortColumn {
			if m.sortDesc {
				columns[i].Title = fmt.Sprintf("%s ▼", col.Title)
			} else {
				columns[i].Title = fmt.Sprintf("%s ▲", col.Title)
			}
		}
	}
	m.table.SetColumns(columns)
}

func (m *model) updateTableRows() {
	var rows []table.Row
	m.filteredPorts = []process.PortInfo{}

	// Filter
	for _, p := range m.ports {
		if m.filter != "" {
			if !strings.Contains(strconv.Itoa(p.Port), m.filter) {
				continue
			}
		}
		m.filteredPorts = append(m.filteredPorts, p)
	}

	// Sort
	if m.sortColumn != "" {
		sort.Slice(m.filteredPorts, func(i, j int) bool {
			p1 := m.filteredPorts[i]
			p2 := m.filteredPorts[j]

			less := false
			switch m.sortColumn {
			case "Port":
				less = p1.Port < p2.Port
			case "Proto":
				less = p1.Protocol < p2.Protocol
			case "PID":
				less = p1.PID < p2.PID
			case "User":
				less = p1.User < p2.User
			case "Lang":
				less = p1.Language < p2.Language
			case "Command":
				less = p1.Command < p2.Command
			}

			if m.sortDesc {
				return !less
			}
			return less
		})
	}

	// Build Rows
	for _, p := range m.filteredPorts {
		rows = append(rows, table.Row{
			strconv.Itoa(p.Port),
			p.Protocol,
			strconv.Itoa(int(p.PID)),
			p.User,
			p.Language,
			p.Command,
		})
	}
	m.table.SetRows(rows)
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	if m.showModal {
		return m.modalView()
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		MarginLeft(1).
		Render("pman")

	shortcuts := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginLeft(2).
		Render("↑/↓: navigate • : search • ctrl+d: kill • r: refresh • q: quit")

	header := lipgloss.JoinHorizontal(lipgloss.Left, title, shortcuts)

	view := header + "\n" + baseStyle.Render(m.table.View())

	if m.searching {
		view += "\n" + m.textInput.View()
	} else {
		view += m.helpView()
	}

	return view
}

func (m model) helpView() string {
	filterStatus := ""
	if m.filter != "" {
		filterStatus = fmt.Sprintf("[Filter: %s]", m.filter)
	}

	status := m.statusMessage
	if status != "" && filterStatus != "" {
		status += " • "
	}

	content := status + filterStatus
	if content == "" {
		return ""
	}

	return "\n " + lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(content)
}

func (m model) modalView() string {
	dialog := lipgloss.NewStyle().
		Width(50).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(1, 2).
		Align(lipgloss.Center)

	question := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("196")).
		Render(fmt.Sprintf("KILL PROCESS %d (%s)?", m.selectedPort.PID, m.selectedPort.Command))

	detail := fmt.Sprintf("Port: %d | User: %s", m.selectedPort.Port, m.selectedPort.User)

	buttons := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("[Y]es"),
		"   ",
		lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("[N]o"),
	)

	ui := lipgloss.JoinVertical(lipgloss.Center,
		question,
		"\n",
		detail,
		"\n\n",
		buttons,
	)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		dialog.Render(ui),
	)
}

func main() {
	columns := []table.Column{
		{Title: "Port", Width: 10},
		{Title: "Proto", Width: 6},
		{Title: "PID", Width: 10},
		{Title: "User", Width: 15},
		{Title: "Lang", Width: 10},
		{Title: "Command", Width: 30},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	ti := textinput.New()
	ti.Placeholder = "Enter port number..."
	ti.Focus()
	ti.CharLimit = 20
	ti.Width = 20

	m := model{
		table:     t,
		textInput: ti,
	}

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
