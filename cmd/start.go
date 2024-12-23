/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"net/http"

	"github.com/campbel/tiny-tunnel/core"
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
		ctx := cmd.Context()

		clientTunnel := core.NewClientTunnel(core.ClientOptions{
			Target:            target,
			Name:              name,
			ServerHost:        serverHost,
			ServerPort:        serverPort,
			Insecure:          insecure,
			AllowedIPs:        allowedIPs,
			ReconnectAttempts: reconnectAttempts,
			TargetHeaders:     convertMapToHeaders(targetHeaders),
			ServerHeaders:     convertMapToHeaders(serverHeaders),
		})

		if err := clientTunnel.Connect(ctx); err != nil {
			return err
		}

		clientTunnel.Wait()

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

func convertMapToHeaders(m map[string]string) http.Header {
	headers := http.Header{}
	for k, v := range m {
		headers.Add(k, v)
	}
	return headers
}
