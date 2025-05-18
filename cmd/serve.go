/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/campbel/tiny-tunnel/core/server"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/spf13/cobra"
)

var (
	port         string
	hostname     string
	enableAuth   bool
	accessPort   string
	accessScheme string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewBasicLogger(os.Getenv("DEBUG") == "true")
		logger.Info("starting server", "port", port, "hostname", hostname)

		ctx := cmd.Context()

		router := server.NewHandler(server.Options{
			Hostname:     hostname,
			EnableAuth:   enableAuth,
			AccessScheme: accessScheme,
			AccessPort:   accessPort,
		}, logger)

		server := &http.Server{
			Addr:    ":" + port,
			Handler: router,
		}

		go func() {
			if err := server.ListenAndServe(); err != nil {
				logger.Error("error starting server", "err", err)
			}
		}()

		<-ctx.Done()
		logger.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			if err == http.ErrServerClosed {
				logger.Info("server closed")
			} else {
				logger.Error("error shutting down server", "err", err)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVarP(&port, "port", "p", "8080", "Port to listen on")
	serveCmd.Flags().StringVarP(&hostname, "hostname", "", "localhost", "Hostname to listen on")
	serveCmd.Flags().BoolVarP(&enableAuth, "enable-auth", "", false, "Enable authentication")
	serveCmd.Flags().StringVarP(&accessPort, "access-port", "", "", "Port to access the tunnel on")
	serveCmd.Flags().StringVarP(&accessScheme, "access-scheme", "", "https", "Scheme to access the tunnel on")
}
