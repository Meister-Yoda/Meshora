package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var errHTTPHealth = errors.New("local HTTP health check failed")

func init() {
	healthCmd.Flags().Bool("http", false, "Check the local HTTP listener, including initial setup")
	rootCmd.AddCommand(healthCmd)
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check the health of the Headscale server",
	Long:  "Check the health of the Headscale server. This command will return an exit code of 0 if the server is healthy, or 1 if it is not.",
	RunE: func(cmd *cobra.Command, args []string) error {
		useHTTP, _ := cmd.Flags().GetBool("http")
		if useHTTP {
			return localHTTPHealth(cmd)
		}

		return withClient(func(ctx context.Context, client *clientv1.ClientWithResponses) error {
			resp, err := client.HealthWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("checking health: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200, "")
		})
	},
}

func localHTTPHealth(cmd *cobra.Command) error {
	host, port, err := net.SplitHostPort(viper.GetString("listen_addr"))
	if err != nil {
		return fmt.Errorf("health listener: %w", err)
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	request, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/health", nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("checking HTTP health: %w", err)
	}
	defer response.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", errHTTPHealth, response.StatusCode)
	}

	return nil
}
