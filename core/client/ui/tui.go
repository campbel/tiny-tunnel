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
			Foreground(lipgloss.Color("#00FF66")).
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
	state           stats.StateProvider
	stats           stats.StatsProvider
	logs            []LogEntry
	program         *tea.Program
	viewport        viewport.Model
	width, height   int
	ready           bool
	lastRefreshTime time.Time
	debug           bool
}

// tickMsg is a message that is sent on each tick interval.
type tickMsg time.Time

// stateUpdateMsg is sent when the TunnelState is updated.
type stateUpdateMsg struct{}

// NewTUI creates a new TUI instance for the given tunnel state.
func NewTUI(state stats.StateProvider, stats stats.StatsProvider) *TUI {
	tui := &TUI{
		state: state,
		stats: stats,
	}
	return tui
}

// Start initializes and starts the TUI.
func (t *TUI) Start() error {
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

		// Update viewport for logs - we need less space for header now
		headerHeight := 10 // Approximate height of header content
		footerHeight := 2  // Height of footer
		t.viewport = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
		t.viewport.Style = lipgloss.NewStyle().
			BorderForeground(lipgloss.Color("#555555")).
			Border(lipgloss.RoundedBorder()).
			Padding(0, 0, 0, 1)

		// Update log content in viewport
		t.viewport.SetContent(t.renderLogs())

	case tickMsg:
		// Periodic refresh (every second)
		t.lastRefreshTime = time.Time(msg)

		// Always update the logs content
		t.viewport.SetContent(t.renderLogs())

		return t, tea.Tick(time.Second, func(time.Time) tea.Msg {
			return tickMsg(time.Now())
		})

	case stateUpdateMsg:
		// Handle state updates
		t.viewport.SetContent(t.renderLogs())
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
	httpStats := t.stats.GetHttpStats()

	// ASCII art title
	asciiTitle := `
 _____ _               _____                        _
/__   (_)_ __  _   _  /__   \_   _ _ __  _ __   ___| |
  / /\/ | '_ \| | | |   / /\/ | | | '_ \| '_ \ / _ \ |
 / /  | | | | | |_| |  / /  | |_| | | | | | | |  __/ |
 \/   |_|_| |_|\__, |  \/    \__,_|_| |_|_| |_|\___|_|
               |___/
`

	// Create simplified status info with minimal text
	var statusElements []string

	// Include only the essential info
	statusElements = append(statusElements, statusStyled)

	// Only include URL if it exists and it's short enough
	if t.state.GetURL() != "" {
		url := t.state.GetURL()
		statusElements = append(statusElements, highlightStyle.Render(url))
	}

	// Calculate dimensions
	titleWidth := 60 // Approximate width of the ASCII art title
	statusWidth := t.width - titleWidth - 4
	if statusWidth < 20 {
		statusWidth = 20 // Minimum width for status panel
	}

	// Create a single status line with all elements joined by dots
	statusLine := strings.Join(statusElements, " • ")

	// Add padding to align at bottom (only if there's room)
	// Fallback if title is too short for padding
	statusDisplay := lipgloss.NewStyle().
		Width(statusWidth).
		Render(statusLine)

	header := lipgloss.JoinVertical(lipgloss.Top,
		titleStyle.Render(asciiTitle),
		statusDisplay)

	// Create simplified metrics bar with nicer styling
	metricsBar := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#333333")).
		Bold(true).
		Padding(0, 2).
		Align(lipgloss.Center).
		Width(t.width).
		Render(fmt.Sprintf("✓ %d Requests Processed", httpStats.TotalRequests))

	// Show logs
	logContent := t.viewport.View()

	// Footer with instructions
	footer := infoStyle.Render("\nPress 'q' to quit, 'o' to open tunnel URL in browser")

	// Combine all parts
	return fmt.Sprintf("%s\n%s\n%s\n%s",
		header,
		metricsBar,
		logContent,
		footer)
}

// renderLogs creates a formatted string of all logs.
func (t *TUI) renderLogs() string {
	if len(t.logs) == 0 {
		return "No logs available"
	}

	// Only show the last 1000 logs
	end := len(t.logs) - 1000
	if end < 0 {
		end = 0
	}

	var sb strings.Builder
	for i := len(t.logs) - 1; i >= end; i-- {
		entry := t.logs[i]
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
		sb.WriteString(fmt.Sprintf("[%s] %s: %s %s\n", timestamp, levelStr, entry.Message, strings.Join(entry.Args, " ")))
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

// TUI should support logger interface
const (
	logDebug = "DEBUG"
	logInfo  = "INFO"
	logWarn  = "WARN"
	logError = "ERROR"
)

type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
	Args      []string
}

func (t *TUI) Log(level string, message string, args ...any) {
	// we want args to be kvps
	kvps := []string{}
	for i := 0; i < len(args); i += 2 {
		kvps = append(kvps, fmt.Sprintf("%s=%v", args[i], args[i+1]))
	}

	t.logs = append(t.logs, LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Args:      kvps,
	})
}

func (t *TUI) Debug(message string, args ...any) {
	if t.debug {
		t.Log(logDebug, message, args...)
	}
}

func (t *TUI) Info(message string, args ...any) {
	t.Log(logInfo, message, args...)
}

func (t *TUI) Warn(message string, args ...any) {
	t.Log(logWarn, message, args...)
}

func (t *TUI) Error(message string, args ...any) {
	t.Log(logError, message, args...)
}
