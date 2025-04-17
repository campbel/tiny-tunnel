/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/spf13/cobra"
)

var (
	target            string
	name              string
	serverHost        string
	serverPort        string
	insecure          bool
	allowedIPs        []string
	reconnectAttempts int
	targetHeaders     map[string]string
	serverHeaders     map[string]string
	token             string
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Set up options with provided parameters
		options := client.Options{
			Target:            target,
			Name:              name,
			ServerHost:        serverHost,
			ServerPort:        serverPort,
			Insecure:          insecure,
			AllowedIPs:        allowedIPs,
			ReconnectAttempts: reconnectAttempts,
			TargetHeaders:     convertMapToHeaders(targetHeaders),
			ServerHeaders:     convertMapToHeaders(serverHeaders),
			Token:             token,
		}

		// If server host is not specified, try to use the default from config
		if serverHost == "" {
			if serverInfo, err := options.GetServerInfo(); err == nil {
				log.Info("using default server from config", "server", serverInfo.Hostname)
				options.ServerHost = serverInfo.Hostname

				// Determine if insecure
				if insecure || serverInfo.Protocol == "http" {
					options.Insecure = true
				} else {
					options.Insecure = false
					options.ServerPort = "443"
				}

				// Use port from config if specified
				if serverInfo.Port != "" {
					options.ServerPort = serverInfo.Port
				}
			}
		}

		log.Info("connecting...", "server", options.ServerHost, "port", options.ServerPort, "insecure", options.Insecure)
	LOOP:
		for i := 0; i < reconnectAttempts; i++ {
			select {
			case <-cmd.Context().Done():
				break LOOP
			default:
				tunnel, err := client.NewTunnel(cmd.Context(), options)
				if err != nil {
					log.Error("error connecting to tunnel", "err", err)
					time.Sleep(3 * time.Second)
					continue
				}
				log.Info("connected", "server", options.ServerHost, "port", options.ServerPort, "insecure", options.Insecure)
				tunnel.Listen(cmd.Context())
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().StringVarP(&target, "target", "t", "", "Target to forward requests to")
	startCmd.Flags().StringVarP(&name, "name", "n", "", "Name of the client")
	startCmd.Flags().StringVarP(&serverHost, "server-host", "s", "", "Host of the server (if empty, uses default from config)")
	startCmd.Flags().StringVarP(&serverPort, "server-port", "p", "", "Port of the server (if empty, uses default from config)")
	startCmd.Flags().BoolVarP(&insecure, "insecure", "i", false, "Use insecure connection to the server")
	startCmd.Flags().StringSliceVarP(&allowedIPs, "allowed-ips", "a", []string{"0.0.0.0/0", "::/0"}, "Allowed IPs")
	startCmd.Flags().IntVarP(&reconnectAttempts, "reconnect-attempts", "r", 5, "Reconnect attempts")
	startCmd.Flags().StringToStringVarP(&targetHeaders, "target-headers", "T", map[string]string{}, "Target headers")
	startCmd.Flags().StringToStringVarP(&serverHeaders, "server-headers", "S", map[string]string{}, "Server headers")
	startCmd.Flags().StringVar(&token, "token", "", "JWT authentication token")
}

func getTunnelAddress(options client.Options) string {
	// Extract hostname and port
	host := options.ServerHost
	port := options.ServerPort

	// Parse hostname if it contains port
	hostParts := strings.Split(host, ":")
	if len(hostParts) > 1 {
		host = hostParts[0]
		// Use explicit port or the port from hostname
		if port == "" {
			port = hostParts[1]
		}
	}

	// Get port from config if not specified
	if port == "" {
		if serverInfo, err := options.GetServerInfo(); err == nil && serverInfo.Port != "" {
			port = serverInfo.Port
		} else {
			// Default ports
			if options.Insecure {
				port = "80"
			} else {
				port = "443"
			}
		}
	}

	scheme := "https"
	if options.Insecure {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s.%s:%s", scheme, options.Name, host, port)
}

func convertMapToHeaders(m map[string]string) http.Header {
	headers := http.Header{}
	for k, v := range m {
		headers.Add(k, v)
	}
	return headers
}
