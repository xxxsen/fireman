// Command fireman is the Fireman backend HTTP server. CLI uses Cobra:
//
//	fireman run --config=./config.json
//	fireman healthcheck
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/fireman/fireman/internal/app"
	"github.com/fireman/fireman/internal/config"
	"github.com/spf13/cobra"
)

var errHealthcheckStatus = errors.New("healthcheck returned non-200 status")

func main() {
	rootCmd := &cobra.Command{
		Use:   "fireman",
		Short: "Fireman FIRE simulation backend",
	}
	rootCmd.AddCommand(newRunCmd(), newHealthcheckCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRunCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the API server and background worker",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return app.Run(context.Background(), cfg)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to JSON config file (required)")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func newHealthcheckCmd() *cobra.Command {
	var healthURL string
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Probe local /healthz and exit 0 on success",
		RunE: func(_ *cobra.Command, _ []string) error {
			return probeHealth(healthURL)
		},
	}
	cmd.Flags().StringVar(&healthURL, "url", "http://127.0.0.1:8080/healthz", "URL probed by healthcheck")
	return cmd
}

func probeHealth(url string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("healthcheck request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status=%d: %w", resp.StatusCode, errHealthcheckStatus)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
