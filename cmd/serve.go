/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/core/server"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/spf13/cobra"
)

var (
	port     string
	hostname string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Info("starting server", "port", port, "hostname", hostname)

		ctx := cmd.Context()

		router := server.NewHandler(server.Options{
			Hostname: hostname,
		})

		server := &http.Server{
			Addr:    ":" + port,
			Handler: router,
		}

		go func() {
			if err := server.ListenAndServe(); err != nil {
				log.Error("error starting server", "err", err)
			}
		}()

		<-ctx.Done()
		log.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			if err == http.ErrServerClosed {
				log.Info("server closed")
			} else {
				log.Error("error shutting down server", "err", err)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVarP(&port, "port", "p", "8080", "Port to listen on")
	serveCmd.Flags().StringVarP(&hostname, "hostname", "", "localhost", "Hostname to listen on")
}
