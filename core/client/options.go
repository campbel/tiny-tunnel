package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ServerConfig holds configuration for a specific server
type ServerConfig struct {
	AuthToken string `json:"auth_token"`
	Port      string `json:"port,omitempty"`     // Optional port number
	Protocol  string `json:"protocol,omitempty"` // http or https
}

// ConfigFile represents the auth.json file structure
type ConfigFile struct {
	Current string                  `json:"current,omitempty"` // Current default server
	Servers map[string]ServerConfig `json:"servers"`
}

// getConfigFilePath returns the path to the auth.json config file
func getConfigFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".config", "tiny-tunnel", "auth.json")
}

// loadConfig loads the config file
func loadConfig() (ConfigFile, error) {
	configPath := getConfigFilePath()
	config := ConfigFile{
		Servers: make(map[string]ServerConfig),
	}

	// If config file doesn't exist, return empty config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return config, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return config, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// GetConfig is the exported version of loadConfig
func GetConfig() (ConfigFile, error) {
	return loadConfig()
}

// saveConfig saves the config file
func saveConfig(config ConfigFile) error {
	configPath := getConfigFilePath()

	// Create directory if it doesn't exist
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ServerInfo contains complete information about a server
type ServerInfo struct {
	Hostname  string
	Port      string
	Protocol  string
	AuthToken string
}

// loadTokenFromConfig loads the token for a specific server from the config file
func loadTokenFromConfig(server string) (string, error) {
	serverInfo, err := getServerInfo(server)
	if err != nil {
		return "", err
	}
	return serverInfo.AuthToken, nil
}

// getServerInfo retrieves server information from config
func getServerInfo(server string) (ServerInfo, error) {
	// If server is empty, use the current default server
	if server == "" {
		return getCurrentServerInfo()
	}

	// Parse the server string to extract hostname and port
	var hostname, port string

	// Check if it's a full URL
	if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
		parsedURL, err := url.Parse(server)
		if err == nil {
			hostname = parsedURL.Hostname()
			port = parsedURL.Port()
			// We don't need protocol here, as it's already stored in the server config
		}
	} else if strings.Contains(server, ":") {
		// Handle hostname:port without protocol
		parts := strings.Split(server, ":")
		hostname = parts[0]
		if len(parts) > 1 {
			port = parts[1]
		}
	} else {
		// Just hostname
		hostname = server
	}

	config, err := loadConfig()
	if err != nil {
		return ServerInfo{}, err
	}

	// Create a lookup key including port if provided
	serverKey := hostname
	if port != "" {
		serverKey = fmt.Sprintf("%s:%s", hostname, port)
	}

	// Try to find exact match with hostname:port
	for configServer, serverConfig := range config.Servers {
		// Check if stored server has port info
		storedPort := serverConfig.Port

		// Scenario 1: Direct match with the stored key
		if configServer == serverKey || configServer == hostname {
			return ServerInfo{
				Hostname:  configServer,
				Port:      serverConfig.Port,
				Protocol:  serverConfig.Protocol,
				AuthToken: serverConfig.AuthToken,
			}, nil
		}

		// Scenario 2: Match hostname and port separately
		// If stored server is just hostname but has port config matching our port
		if configServer == hostname && storedPort == port {
			return ServerInfo{
				Hostname:  hostname,
				Port:      storedPort,
				Protocol:  serverConfig.Protocol,
				AuthToken: serverConfig.AuthToken,
			}, nil
		}

		// Scenario 3: Match with hostname:port format
		configParts := strings.Split(configServer, ":")
		configHostname := configParts[0]
		var configPort string
		if len(configParts) > 1 {
			configPort = configParts[1]
		} else {
			configPort = storedPort
		}

		if configHostname == hostname && (port == "" || configPort == port) {
			return ServerInfo{
				Hostname:  configHostname,
				Port:      configPort,
				Protocol:  serverConfig.Protocol,
				AuthToken: serverConfig.AuthToken,
			}, nil
		}
	}

	return ServerInfo{}, fmt.Errorf("server not found in config")
}

// getCurrentServerInfo gets the current default server information
func getCurrentServerInfo() (ServerInfo, error) {
	config, err := loadConfig()
	if err != nil {
		return ServerInfo{}, err
	}

	if config.Current == "" {
		return ServerInfo{}, fmt.Errorf("no current server set")
	}

	serverConfig, ok := config.Servers[config.Current]
	if !ok {
		return ServerInfo{}, fmt.Errorf("current server not found in config")
	}

	return ServerInfo{
		Hostname:  config.Current,
		Port:      serverConfig.Port,
		Protocol:  serverConfig.Protocol,
		AuthToken: serverConfig.AuthToken,
	}, nil
}

// SetDefaultServer sets the current default server
func SetDefaultServer(server string) error {
	// Parse server information
	var hostname, port string

	if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
		parsedURL, err := url.Parse(server)
		if err == nil {
			hostname = parsedURL.Hostname()
			port = parsedURL.Port()
		}
	} else if strings.Contains(server, ":") {
		// Handle case of hostname:port without protocol
		parts := strings.Split(server, ":")
		hostname = parts[0]
		if len(parts) > 1 {
			port = parts[1]
		}
	} else {
		// Just hostname
		hostname = server
	}

	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create lookup key with port if provided
	lookupKey := hostname
	if port != "" {
		lookupKey = fmt.Sprintf("%s:%s", hostname, port)
	}

	// First try exact match with key
	if _, ok := config.Servers[lookupKey]; ok {
		config.Current = lookupKey
		return saveConfig(config)
	}

	// Try to find a matching server with any port
	foundKey := ""
	for configServer := range config.Servers {
		configParts := strings.Split(configServer, ":")
		configHostname := configParts[0]

		// If we have a port, match hostname and port
		if port != "" {
			if configHostname == hostname && len(configParts) > 1 && configParts[1] == port {
				foundKey = configServer
				break
			}
		} else if configHostname == hostname {
			// If no port specified, match just by hostname
			foundKey = configServer
			break
		}
	}

	if foundKey == "" {
		return fmt.Errorf("server '%s' not found in config, please login first", server)
	}

	// Set as current server
	config.Current = foundKey

	return saveConfig(config)
}

// SaveTokenToConfig saves a token for a server to the config file
func SaveTokenToConfig(server, token string) error {
	// Extract hostname, port, and protocol if a full URL was provided
	var hostname, port, protocol string

	// Parse server information
	if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
		parsedURL, err := url.Parse(server)
		if err == nil {
			hostname = parsedURL.Hostname()
			port = parsedURL.Port()
			protocol = parsedURL.Scheme
		}
	} else if strings.Contains(server, ":") {
		// Handle case of hostname:port without protocol
		parts := strings.Split(server, ":")
		hostname = parts[0]
		if len(parts) > 1 {
			port = parts[1]
		}
		// Default protocol based on hostname
		if hostname == "localhost" || strings.HasPrefix(hostname, "127.0.0.1") {
			protocol = "http"
		} else {
			protocol = "https"
		}
	} else {
		// Just hostname
		hostname = server
		// Default protocol based on hostname
		if hostname == "localhost" || strings.HasPrefix(hostname, "127.0.0.1") {
			protocol = "http"
		} else {
			protocol = "https"
		}
	}

	config, err := loadConfig()
	if err != nil {
		config = ConfigFile{
			Servers: make(map[string]ServerConfig),
		}
	}

	// Create the server key for storage
	// Include port in the key if present
	storageKey := hostname
	if port != "" {
		storageKey = fmt.Sprintf("%s:%s", hostname, port)
	}

	// Store server config with all details
	config.Servers[storageKey] = ServerConfig{
		AuthToken: token,
		Port:      port,
		Protocol:  protocol,
	}

	// Set as current
	config.Current = storageKey

	return saveConfig(config)
}

type Options struct {
	Target            string
	Name              string
	ServerHost        string
	ServerPort        string
	Insecure          bool
	AllowedIPs        []string
	ReconnectAttempts int
	TargetHeaders     http.Header
	ServerHeaders     http.Header
	Token             string // JWT auth token

	OutputWriter io.Writer
}

func (c Options) Origin() string {
	// Extract hostname without port
	host := c.ServerHost
	hostParts := strings.Split(host, ":")
	if len(hostParts) > 1 {
		host = hostParts[0]
	}

	return c.SchemeHTTP() + "://" + host
}

func (c Options) URL() string {
	// Extract hostname and port if serverHost already contains port info
	host := c.ServerHost
	port := c.ServerPort

	// Check if host already contains a port
	hostParts := strings.Split(host, ":")
	if len(hostParts) > 1 {
		// Host already contains a port, extract it
		host = hostParts[0]
		// Use explicit port from options if provided, otherwise use port from hostname
		if port == "" {
			port = hostParts[1]
		}
	} else if port == "" {
		// If port is still empty, try to get it from the config
		if serverInfo, err := c.GetServerInfo(); err == nil && serverInfo.Port != "" {
			port = serverInfo.Port
		} else {
			// Default ports based on scheme
			if c.Insecure {
				port = "80"
			} else {
				port = "443"
			}
		}
	}

	url := c.SchemeWS() + "://" + host + ":" + port + "/register?name=" + c.Name
	return url
}

func (c Options) SchemeHTTP() string {
	// Try to get protocol from server info
	if serverInfo, err := c.GetServerInfo(); err == nil && serverInfo.Protocol != "" {
		return serverInfo.Protocol
	}

	// Fall back to using the insecure flag
	if c.Insecure {
		return "http"
	}
	return "https"
}

func (c Options) SchemeWS() string {
	// Base the WebSocket scheme on the HTTP scheme
	httpScheme := c.SchemeHTTP()
	if httpScheme == "http" {
		return "ws"
	}
	return "wss"
}

func (c Options) Output() io.Writer {
	if c.OutputWriter == nil {
		return os.Stdout
	}
	return c.OutputWriter
}

// GetResolvedToken returns the token based on priority:
// 1. Explicit token from options
// 2. Environment variable
// 3. Config file (using server host from options or default)
func (c Options) GetResolvedToken() string {
	// Priority 1: Use explicit token from options if provided
	if c.Token != "" {
		return c.Token
	}

	// Priority 2: Check environment variable
	if envToken := os.Getenv("TINY_TUNNEL_CLIENT_TOKEN"); envToken != "" {
		return envToken
	}

	// Priority 3: Check config file
	token, err := loadTokenFromConfig(c.ServerHost)
	if err == nil && token != "" {
		return token
	}

	return ""
}

// GetServerInfo returns complete server information
// If ServerHost is not set, it will use the current default server
func (c Options) GetServerInfo() (ServerInfo, error) {
	return getServerInfo(c.ServerHost)
}

func (c Options) Valid() error {
	var errs []error
	if c.Name == "" {
		errs = append(errs, fmt.Errorf("name is required"))
	}
	if c.Target == "" {
		errs = append(errs, fmt.Errorf("target is required"))
	}
	for _, ip := range c.AllowedIPs {
		if _, _, err := net.ParseCIDR(ip); err != nil {
			errs = append(errs, fmt.Errorf("invalid IP CIDR range specified: %s", ip))
		}
	}
	var finalErr error
	for _, err := range errs {
		if finalErr == nil {
			finalErr = err
		} else {
			finalErr = fmt.Errorf("%w\n%s", finalErr, err)
		}
	}
	return finalErr
}
