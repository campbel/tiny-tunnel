/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/campbel/tiny-tunnel/server"
	"github.com/spf13/cobra"
)

var (
	port        string
	hostname    string
	letsEncrypt bool
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		server.Serve(server.ServeOptions{
			Port:        port,
			Hostname:    hostname,
			LetsEncrypt: letsEncrypt,
		})
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVarP(&port, "port", "p", "8080", "Port to listen on")
	serveCmd.Flags().StringVarP(&hostname, "hostname", "", "localhost", "Hostname to listen on")
	serveCmd.Flags().BoolVarP(&letsEncrypt, "lets-encrypt", "l", false, "Use lets encrypt")
}
