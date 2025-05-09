/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"net/http"
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
	enableTUI         bool
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a tunnel connection",
	Long:  `Start a tunnel connection to expose a local service to the internet.`,
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
		
		// Create and establish the tunnel connection
		tunnel, err := client.NewTunnel(cmd.Context(), options)
		if err != nil {
			log.Error("error connecting to tunnel", "err", err)
			return err
		}
		
		log.Info("connected", "server", options.ServerHost, "port", options.ServerPort, "insecure", options.Insecure)
		
		// If TUI is enabled, start it in a separate goroutine before entering the listen loop
		if enableTUI {
			go func() {
				if err := client.StartTUI(cmd.Context(), tunnel); err != nil {
					log.Error("error starting TUI", "err", err)
				}
			}()
			
			// TUI handles the context cancellation for proper shutdown
			tunnel.Listen(cmd.Context())
		} else {
			// Standard reconnection loop without TUI
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
	startCmd.Flags().BoolVarP(&enableTUI, "tui", "u", true, "Enable Terminal User Interface")
}

func convertMapToHeaders(m map[string]string) http.Header {
	headers := http.Header{}
	for k, v := range m {
		headers.Add(k, v)
	}
	return headers
}