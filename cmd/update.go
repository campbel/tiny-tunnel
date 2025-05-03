package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	log "github.com/charmbracelet/log"
	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"
)

// Release represents a GitHub release
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents an asset in a GitHub release
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update tnl to the latest version",
	Run: func(cmd *cobra.Command, args []string) {
		if err := performUpdate(); err != nil {
			log.Errorf("Update failed: %v", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func performUpdate() error {
	log.Info("Checking for updates...")

	// Get the latest release from GitHub
	latestRelease, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to get latest release: %w", err)
	}

	// Extract version from the tag name (remove 'v' prefix)
	latestVersion := strings.TrimPrefix(latestRelease.TagName, "v")
	log.Infof("Latest version: %s", latestVersion)

	// Determine OS and architecture for the download
	osName := runtime.GOOS
	archName := runtime.GOARCH

	// Find the appropriate asset to download
	assetName := fmt.Sprintf("tnl-%s-%s", osName, archName)
	var downloadURL string
	for _, asset := range latestRelease.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no release found for %s/%s", osName, archName)
	}

	// Get the current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	log.Debugf("Current executable: %s", execPath)

	// Download the update
	log.Infof("Downloading update from %s", downloadURL)
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download update, status: %s", resp.Status)
	}

	// Read the response body
	update, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read update data: %w", err)
	}

	// Apply the update
	log.Info("Installing update...")
	err = selfupdate.Apply(bytes.NewReader(update), selfupdate.Options{})
	if err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			log.Errorf("Failed to rollback from bad update: %v", rerr)
		}
		return fmt.Errorf("failed to apply update: %w", err)
	}

	log.Infof("Successfully updated tnl to version %s", latestVersion)
	return nil
}

// getLatestRelease fetches the latest release from GitHub
func getLatestRelease() (Release, error) {
	url := "https://api.github.com/repos/campbel/tiny-tunnel/releases/latest"
	
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "tiny-tunnel-updater")
	
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("failed to get latest release, status: %s", resp.Status)
	}
	
	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return release, nil
}