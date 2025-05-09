package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/campbel/tiny-tunnel/core/stats"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	// Styles for different UI elements
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#25A065")).
			Bold(true).
			Padding(0, 1)

	statusConnectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#25A065")).
				Bold(true)

	statusConnectingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFAA33")).
				Bold(true)

	statusDisconnectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF4433")).
				Bold(true)

	statusErrorStyle = lipgloss.NewStyle().
			   Foreground(lipgloss.Color("#FF0000")).
			   Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))

	highlightStyle = lipgloss.NewStyle().
			  Foreground(lipgloss.Color("#25A065")).
			  Bold(true)

	subtitleStyle = lipgloss.NewStyle().
			 Foreground(lipgloss.Color("#CCCCCC")).
			 Padding(0, 0, 1, 2)

	boxStyle = lipgloss.NewStyle().
		   Border(lipgloss.RoundedBorder()).
		   BorderForeground(lipgloss.Color("#555555")).
		   Padding(0, 1)

	// Log level styles
	logLevelInfo = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#25A065"))

	logLevelError = lipgloss.NewStyle().
			 Foreground(lipgloss.Color("#FF0000"))

	logLevelWarn = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFAA33"))

	logLevelDebug = lipgloss.NewStyle().
			 Foreground(lipgloss.Color("#888888"))
)

// TUI is the main terminal UI component using Bubbletea.
type TUI struct {
	state           *stats.TunnelState
	logCapture      *LogCapture
	program         *tea.Program
	viewport        viewport.Model
	width, height   int
	ready           bool
	lastRefreshTime time.Time
	statusBar       string
}

// tickMsg is a message that is sent on each tick interval.
type tickMsg time.Time

// stateUpdateMsg is sent when the TunnelState is updated.
type stateUpdateMsg struct{}

// NewTUI creates a new TUI instance for the given tunnel state.
func NewTUI(state *stats.TunnelState) *TUI {
	tui := &TUI{
		state:      state,
		logCapture: NewLogCapture(state),
	}
	return tui
}

// Start initializes and starts the TUI.
func (t *TUI) Start() error {
	// Start capturing logs
	t.logCapture.Start()
	defer t.logCapture.Stop()

	// Subscribe to state updates
	t.state.Subscribe(t)

	// Initialize the program
	p := tea.NewProgram(t, tea.WithAltScreen())
	t.program = p

	// Run the program
	_, err := p.Run()
	return err
}

// OnStateUpdate implements the StateSubscriber interface.
func (t *TUI) OnStateUpdate(state *stats.TunnelState) {
	if t.program != nil {
		t.program.Send(stateUpdateMsg{})
	}
}

// Init initializes the TUI model.
func (t *TUI) Init() tea.Cmd {
	// Start a ticker to refresh the UI
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return tickMsg(time.Now())
	})
}

// Update handles UI updates based on messages.
func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle keyboard shortcuts
		switch msg.String() {
		case "ctrl+c", "q":
			// Quit the application
			return t, tea.Quit

		case "l":
			// Toggle log view mode
			t.state.ToggleLogViewMode()
			return t, nil

		case "o":
			// Open the tunnel URL in a browser
			go t.openURL(t.state.GetURL())
			return t, nil
		}

	case tea.WindowSizeMsg:
		// Handle window resize
		t.width = msg.Width
		t.height = msg.Height
		t.ready = true

		// Update viewport for logs
		headerHeight := 12 // Approximate height of header content
		footerHeight := 2  // Height of footer
		t.viewport = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
		t.viewport.Style = lipgloss.NewStyle().
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 0, 0, 1)

		// Update log content in viewport
		if t.state.IsLogViewMode() {
			t.viewport.SetContent(t.renderLogs())
		}

	case tickMsg:
		// Periodic refresh (every second)
		t.lastRefreshTime = time.Time(msg)
		
		return t, tea.Tick(time.Second, func(time.Time) tea.Msg {
			return tickMsg(time.Now())
		})

	case stateUpdateMsg:
		// Handle state updates
		if t.state.IsLogViewMode() {
			t.viewport.SetContent(t.renderLogs())
		}
	}

	// Handle viewport updates
	t.viewport, cmd = t.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return t, tea.Batch(cmds...)
}

// View renders the UI.
func (t *TUI) View() string {
	if !t.ready {
		return "Initializing..."
	}

	// Format connection status
	statusText := string(t.state.GetStatus())
	var statusStyled string

	switch t.state.GetStatus() {
	case stats.StatusConnected:
		statusStyled = statusConnectedStyle.Render(statusText)
	case stats.StatusConnecting:
		statusStyled = statusConnectingStyle.Render(statusText)
	case stats.StatusDisconnected:
		statusStyled = statusDisconnectedStyle.Render(statusText)
	case stats.StatusError:
		statusStyled = statusErrorStyle.Render(statusText)
	}

	// Get metrics
	statsData := t.state.GetTracker()
	wsStats := statsData.GetWebsocketStats()
	httpStats := statsData.GetHttpStats()
	sseStats := statsData.GetSseStats()

	// Build header info
	headerInfo := []string{
		fmt.Sprintf("Status: %s", statusStyled),
		fmt.Sprintf("Name: %s", highlightStyle.Render(t.state.GetName())),
		fmt.Sprintf("Target: %s", t.state.GetTarget()),
	}

	if t.state.GetURL() != "" {
		headerInfo = append(headerInfo, fmt.Sprintf("URL: %s", highlightStyle.Render(t.state.GetURL())))
	}

	if t.state.GetStatus() == stats.StatusConnected {
		duration := t.state.GetConnectionDuration().Round(time.Second)
		headerInfo = append(headerInfo, fmt.Sprintf("Connected for: %s", duration))
	}

	// Create metrics display
	metrics := []string{
		fmt.Sprintf("HTTP Requests: %d", httpStats.TotalRequests),
		fmt.Sprintf("HTTP Responses: %d", httpStats.TotalResponses),
		fmt.Sprintf("WebSocket Connections: %d (active: %d)", wsStats.TotalConnections, wsStats.ActiveConnections),
		fmt.Sprintf("WebSocket Messages Sent: %d", wsStats.TotalMessagesSent),
		fmt.Sprintf("WebSocket Messages Received: %d", wsStats.TotalMessagesRecv),
		fmt.Sprintf("SSE Connections: %d (active: %d)", sseStats.TotalConnections, sseStats.ActiveConnections),
		fmt.Sprintf("SSE Messages Received: %d", sseStats.TotalMessagesRecv),
	}

	// Create main view
	var mainContent string

	if t.state.IsLogViewMode() {
		// Show logs view
		mainContent = fmt.Sprintf("%s\n%s", 
			subtitleStyle.Render("Logs (press 'l' to return to stats)"),
			t.viewport.View())
	} else {
		// Show stats view
		headerBox := boxStyle.Render(strings.Join(headerInfo, "\n"))
		metricsBox := boxStyle.Render(strings.Join(metrics, "\n"))
		
		// Show status message if available
		statusMsg := ""
		if msg := t.state.GetStatusMessage(); msg != "" {
			statusMsg = boxStyle.Render(fmt.Sprintf("Message: %s", msg))
		}
		
		mainContent = fmt.Sprintf("%s\n\n%s\n\n%s",
			headerBox,
			metricsBox,
			statusMsg)
	}

	// Footer with instructions
	footer := infoStyle.Render("\nPress 'q' to quit, 'l' to toggle logs, 'o' to open tunnel URL in browser")

	// Combine all parts
	return fmt.Sprintf("%s\n%s\n%s",
		titleStyle.Width(t.width).Render("Tiny Tunnel"),
		mainContent,
		footer)
}

// renderLogs creates a formatted string of all logs.
func (t *TUI) renderLogs() string {
	logs := t.state.GetLogs()
	if len(logs) == 0 {
		return "No logs available"
	}

	var sb strings.Builder
	for _, entry := range logs {
		// Format timestamp
		timestamp := entry.Timestamp.Format("15:04:05")
		
		// Format level with appropriate style
		var levelStr string
		switch strings.ToLower(entry.Level) {
		case "info":
			levelStr = logLevelInfo.Render("INFO")
		case "error":
			levelStr = logLevelError.Render("ERROR")
		case "warn":
			levelStr = logLevelWarn.Render("WARN")
		case "debug":
			levelStr = logLevelDebug.Render("DEBUG")
		default:
			levelStr = entry.Level
		}

		// Format the log line
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", timestamp, levelStr, entry.Message))
	}

	return sb.String()
}

// openURL opens the given URL in the default browser.
func (t *TUI) openURL(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default: // linux, freebsd, etc.
		cmd = exec.Command("xdg-open", url)
	}

	// Start the command but don't wait for it to complete
	_ = cmd.Start()
}