/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/spf13/cobra"
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage tiny-tunnel configuration",
	Long:  `Manage tiny-tunnel configuration, including servers and authentication tokens.`,
}

// serversCmd lists all configured servers
var serversCmd = &cobra.Command{
	Use:   "servers",
	Short: "List all configured servers",
	Long:  `List all configured servers and their details.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config, err := client.GetConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if len(config.Servers) == 0 {
			fmt.Println("No servers configured. Use 'tiny-tunnel login <server>' to add a server.")
			return nil
		}

		// Print servers in table format
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "HOSTNAME\tPORT\tPROTOCOL\tDEFAULT")

		for hostname, serverConfig := range config.Servers {
			isDefault := " "
			if hostname == config.Current {
				isDefault = "*"
			}

			port := serverConfig.Port
			if port == "" {
				port = "443"
			}

			protocol := serverConfig.Protocol
			if protocol == "" {
				protocol = "https"
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", hostname, port, protocol, isDefault)
		}
		w.Flush()

		return nil
	},
}

// setDefaultCmd sets the default server
var setDefaultCmd = &cobra.Command{
	Use:   "set-default [server]",
	Short: "Set the default server",
	Long:  `Set the default server to use when no server is specified.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewBasicLogger(os.Getenv("DEBUG") == "true")
		server := args[0]

		err := client.SetDefaultServer(server)
		if err != nil {
			return fmt.Errorf("failed to set default server: %w", err)
		}

		logger.Info("default server set successfully", "server", server)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(serversCmd)
	configCmd.AddCommand(setDefaultCmd)
}
