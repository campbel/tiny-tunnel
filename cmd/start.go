/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/core"
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
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		options := core.ClientOptions{
			Target:            target,
			Name:              name,
			ServerHost:        serverHost,
			ServerPort:        serverPort,
			Insecure:          insecure,
			AllowedIPs:        allowedIPs,
			ReconnectAttempts: reconnectAttempts,
			TargetHeaders:     convertMapToHeaders(targetHeaders),
			ServerHeaders:     convertMapToHeaders(serverHeaders),
		}
		clientTunnel := core.NewClientTunnel(options)

		log.Info("connecting...")
	LOOP:
		for i := 0; i < reconnectAttempts; i++ {
			select {
			case <-cmd.Context().Done():
				break LOOP
			default:
				if err := clientTunnel.Connect(cmd.Context()); err != nil {
					log.Error("error connecting to tunnel", "err", err)
					time.Sleep(3 * time.Second)
					continue
				}
				log.Info("connected", "address", getTunnelAddress(options))
				clientTunnel.Wait()
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().StringVarP(&target, "target", "t", "", "Target to forward requests to")
	startCmd.Flags().StringVarP(&name, "name", "n", "", "Name of the client")
	startCmd.Flags().StringVarP(&serverHost, "server-host", "s", "tnl.campbel.io", "Host of the server")
	startCmd.Flags().StringVarP(&serverPort, "server-port", "p", "443", "Port of the server")
	startCmd.Flags().BoolVarP(&insecure, "insecure", "i", false, "Use insecure connection to the server")
	startCmd.Flags().StringSliceVarP(&allowedIPs, "allowed-ips", "a", []string{"0.0.0.0/0", "::/0"}, "Allowed IPs")
	startCmd.Flags().IntVarP(&reconnectAttempts, "reconnect-attempts", "r", 5, "Reconnect attempts")
	startCmd.Flags().StringToStringVarP(&targetHeaders, "target-headers", "T", map[string]string{}, "Target headers")
	startCmd.Flags().StringToStringVarP(&serverHeaders, "server-headers", "S", map[string]string{}, "Server headers")
}

func getTunnelAddress(options core.ClientOptions) string {
	if options.Insecure {
		return fmt.Sprintf("http://%s.%s:%s", options.Name, options.ServerHost, options.ServerPort)
	}
	return fmt.Sprintf("https://%s.%s:%s", options.Name, options.ServerHost, options.ServerPort)
}

func convertMapToHeaders(m map[string]string) http.Header {
	headers := http.Header{}
	for k, v := range m {
		headers.Add(k, v)
	}
	return headers
}
