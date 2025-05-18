/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"os"

	"github.com/campbel/tiny-tunnel/internal/echo"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/spf13/cobra"
)

var (
	echoPort string
)

// echoCmd represents the echo command
var echoCmd = &cobra.Command{
	Use:   "echo",
	Short: "Run an http server that echos the request back to the client.",
	Long:  `Run an http server that echos requests back to the client. Supports HTTP, SSE, and WebSocket endpoints.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewBasicLogger(os.Getenv("DEBUG") == "true")
		// Create and configure the echo server
		server, err := echo.NewServer(echo.Options{
			Port: echoPort,
		}, logger)
		if err != nil {
			return err
		}

		// Start the server
		if err := server.Start(); err != nil {
			return err
		}

		// Wait for context cancellation (Ctrl+C)
		<-cmd.Context().Done()

		// Shutdown the server gracefully
		return server.Shutdown(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(echoCmd)
	echoCmd.Flags().StringVarP(&echoPort, "port", "p", "8000", "Port to listen on")
}
