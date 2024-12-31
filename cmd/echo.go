/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Info("starting echo server", "port", echoPort)
		return http.ListenAndServe(":"+echoPort, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Info("handling request", "method", r.Method, "path", r.URL.Path)
			body, _ := io.ReadAll(r.Body)
			response := map[string]any{
				"method":  r.Method,
				"url":     r.URL,
				"headers": r.Header,
				"body":    body,
			}
			data, _ := json.Marshal(response)
			fmt.Fprint(w, string(data))
		}))
	},
}

func init() {
	rootCmd.AddCommand(echoCmd)
	echoCmd.Flags().StringVarP(&echoPort, "port", "p", "8000", "Port to listen on")
}
