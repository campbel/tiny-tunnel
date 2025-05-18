/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

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
  tnl login tnl.campbel.io
  tnl login localhost:8080
  tnl login http://localhost:8080
  tnl login https://example.com:8443`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewBasicLogger(os.Getenv("DEBUG") == "true")
		serverArg := args[0]

		// Parse server into a proper URL
		serverURL, err := parseServerURL(serverArg)
		if err != nil {
			return err
		}

		// Save original server string for config
		originalServer := serverArg

		// Create login URL
		loginURL, err := url.JoinPath(serverURL.String(), "login")
		if err != nil {
			return fmt.Errorf("failed to join path: %w", err)
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
			return err
		}
		fmt.Println()

		token := strings.TrimSpace(string(tokenBytes))
		if token == "" {
			return fmt.Errorf("token cannot be empty")
		}

		// Save token to config with original server string to preserve all details
		if err := client.SaveTokenToConfig(originalServer, token); err != nil {
			logger.Error("failed to save token", "err", err)
			return err
		}

		// Verify token with the auth-test endpoint
		options := client.Options{
			ServerHost: originalServer,
			// Token will be loaded from config automatically
		}

		details, err := client.TestAuth(options)
		if err != nil {
			fmt.Println("\nLogin failed, token verification failed.")
			fmt.Printf("Error: %s\n", err)
			return err
		}

		// Extract and show token details in a friendly way
		fmt.Printf("\nLogin successful! Token verified with the following details:\n")

		if email, ok := details["email"].(string); ok {
			fmt.Printf("- User: %s\n", email)
		}

		if scopes, ok := details["scopes"].([]interface{}); ok && len(scopes) > 0 {
			fmt.Print("- Permissions: ")
			for i, scope := range scopes {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(scope)
			}
			fmt.Println()
		}

		if expiresStr, ok := details["expires"].(string); ok {
			if expires, err := time.Parse(time.RFC3339, expiresStr); err == nil {
				fmt.Printf("- Expires: %s\n", expires.Format("2006-01-02 15:04:05"))
			}
		}

		fmt.Println(`You can now start a tunnel like:

tnl start --name myapp --target http://localhost:8080
    `)

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
