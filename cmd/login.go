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
	"github.com/campbel/tiny-tunnel/internal/guardian"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	loginGuardianURL  string
	loginClientID     string
	loginCallbackPort int
	loginPasteToken   bool
)

// loginCmd represents the login command
var loginCmd = &cobra.Command{
	Use:   "login [server]",
	Short: "Login to a tunnel server via Guardian",
	Long: `Login to a tunnel server. Authentication is handled by Guardian:
a browser window opens for SSO, and the resulting token is stored for the
given tunnel server.

Alternatively pass --paste to enter a credential manually (e.g. a Guardian
API key starting with dch_).

Examples:
  tnl login tnl.stable.dexus.io
  tnl login localhost:8080
  tnl login tnl.stable.dexus.io --paste`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewBasicLogger(os.Getenv("DEBUG") == "true")
		originalServer := args[0]

		var token string
		if loginPasteToken {
			fmt.Println("Paste a Guardian credential (API key dch_... or access token).")
			fmt.Print("Credential: ")
			tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return err
			}
			fmt.Println()
			token = strings.TrimSpace(string(tokenBytes))
			if token == "" {
				return fmt.Errorf("credential cannot be empty")
			}
		} else {
			result, err := guardian.Login(cmd.Context(), guardian.LoginConfig{
				GuardianURL:  loginGuardianURL,
				ClientID:     loginClientID,
				CallbackPort: loginCallbackPort,
				OpenBrowser:  openBrowser,
				AuthURLCallback: func(url string) {
					fmt.Printf("Opening your browser for SSO login...\n\nIf it doesn't open automatically, visit:\n%s\n\n", url)
				},
			})
			if err != nil {
				return fmt.Errorf("guardian login failed: %w", err)
			}
			token = result.AccessToken
			if result.ExpiresIn > 0 {
				fmt.Printf("Received access token (valid for %s).\n", (time.Duration(result.ExpiresIn) * time.Second).String())
			}
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

		fmt.Printf("\nLogin successful! Token verified with the following details:\n")

		if user, ok := details["user"].(string); ok && user != "" {
			fmt.Printf("- User: %s\n", user)
		} else if email, ok := details["email"].(string); ok && email != "" {
			fmt.Printf("- User: %s\n", email)
		}

		if method, ok := details["auth_method"].(string); ok && method != "" {
			fmt.Printf("- Auth method: %s\n", method)
		}

		if expiresStr, ok := details["expires"].(string); ok && expiresStr != "" {
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
	loginCmd.Flags().StringVar(&loginGuardianURL, "guardian-url", "https://id.stable.dexus.io", "Guardian base URL")
	loginCmd.Flags().StringVar(&loginClientID, "client-id", "svc_tiny-tunnel_stable", "Guardian service client ID")
	loginCmd.Flags().IntVar(&loginCallbackPort, "callback-port", 8085, "Localhost port for the OAuth callback (must be registered in Guardian)")
	loginCmd.Flags().BoolVar(&loginPasteToken, "paste", false, "Paste a credential manually instead of the browser SSO flow")
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
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // "linux", "freebsd", etc.
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}
