/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// loginCmd represents the login command
var loginCmd = &cobra.Command{
	Use:   "login [server]",
	Short: "Login to a tunnel server",
	Long: `Login to a tunnel server by opening the auth page in your browser.
Copy the generated token and paste it into the terminal prompt.

Examples:
  tiny-tunnel login tnl.campbel.io
  tiny-tunnel login localhost:8080
  tiny-tunnel login http://localhost:8080
  tiny-tunnel login https://example.com:8443`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverArg := args[0]
		
		// Parse server into a proper URL
		serverURL, err := parseServerURL(serverArg)
		if err != nil {
			return err
		}
		
		// Save original server string for config
		originalServer := serverArg
		
		// Create login URL
		loginURL := serverURL.String()
		if !strings.HasSuffix(loginURL, "/") {
			loginURL += "/login"
		} else {
			loginURL += "login"
		}
		
		fmt.Printf("Opening %s in your browser...\n", loginURL)
		if err := openBrowser(loginURL); err != nil {
			fmt.Printf("Failed to open browser automatically. Please open this URL manually: %s\n", loginURL)
		}
		
		fmt.Println("\nA JWT token has been generated in your browser.")
		fmt.Println("Click the Copy button in the browser, then paste the token here.")
		fmt.Print("Token: ")
		
		// Read token (password-style input for security)
		tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			// Fallback to regular input if terminal is not available
			reader := bufio.NewReader(os.Stdin)
			tokenStr, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read token: %w", err)
			}
			tokenBytes = []byte(strings.TrimSpace(tokenStr))
		} else {
			fmt.Println() // Add a newline after the hidden input
		}
		
		token := strings.TrimSpace(string(tokenBytes))
		if token == "" {
			return fmt.Errorf("token cannot be empty")
		}
		
		// Create a display server for pretty printing (with port if available)
		displayServer := serverURL.Hostname()
		if serverURL.Port() != "" {
			displayServer = fmt.Sprintf("%s:%s", serverURL.Hostname(), serverURL.Port())
		}
		
		// Save token to config with original server string to preserve all details
		if err := client.SaveTokenToConfig(originalServer, token); err != nil {
			log.Error("failed to save token", "err", err)
			return err
		}
		
		log.Info("token saved successfully", "server", displayServer)
		fmt.Printf("\nYou are now logged in to %s.\n", displayServer)
		
		// Check if this is now the default server
		config, _ := client.GetConfig()
		if config.Current == displayServer || strings.HasPrefix(config.Current, serverURL.Hostname()) {
			fmt.Printf("This server is now set as your default. You can use 'tiny-tunnel start' without specifying a server.\n")
		} else {
			fmt.Printf("You can use 'tiny-tunnel start --server-host %s' or set it as your default with:\n  tiny-tunnel config set-default %s\n", 
				displayServer, displayServer)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}

// parseServerURL parses a server string into a URL
func parseServerURL(server string) (*url.URL, error) {
	// Check if server already has a scheme
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		// No scheme provided, check if it's localhost or IP
		if strings.HasPrefix(server, "localhost") || strings.HasPrefix(server, "127.0.0.1") {
			server = "http://" + server
		} else {
			server = "https://" + server
		}
	}
	
	// Parse URL
	parsedURL, err := url.Parse(server)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}
	
	return parsedURL, nil
}

// openBrowser opens the specified URL in the default browser
func openBrowser(url string) error {
	var cmd *exec.Cmd
	
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	
	return cmd.Start()
}