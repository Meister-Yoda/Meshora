package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var errPasswordMismatch = errors.New("passwords do not match")

func init() {
	rootCmd.AddCommand(webAdminsCmd)

	webAdminsCmd.AddCommand(listWebAdminsCmd)

	createWebAdminCmd.Flags().String("password-file", "", "Read the password from this file instead of prompting")
	webAdminsCmd.AddCommand(createWebAdminCmd)

	setWebAdminPasswordCmd.Flags().String("password-file", "", "Read the password from this file instead of prompting")
	webAdminsCmd.AddCommand(setWebAdminPasswordCmd)

	webAdminsCmd.AddCommand(deleteWebAdminCmd)
}

var webAdminsCmd = &cobra.Command{
	Use:     "webadmins",
	Short:   "Manage the local accounts of the web admin dashboard",
	Aliases: []string{"webadmin"},
}

var listWebAdminsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List web admin accounts",
	Aliases: []string{"ls", cmdShow},
	RunE: clientRunE(func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
		resp, err := client.ListWebAdminsWithResponse(ctx)
		if err != nil {
			return fmt.Errorf("listing web admins: %w", err)
		}

		if resp.StatusCode() != http.StatusOK {
			return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
		}

		admins := resp.JSON200.WebAdmins

		return printListOutput(cmd, admins, func() error {
			rows := make([][]string, 0, len(admins))
			for _, admin := range admins {
				lastLogin := "-"
				if admin.LastLoginAt != nil {
					lastLogin = admin.LastLoginAt.Format(HeadscaleDateTimeFormat)
				}

				var created string
				if admin.CreatedAt != nil {
					created = admin.CreatedAt.Format(HeadscaleDateTimeFormat)
				}

				rows = append(rows, []string{admin.Id, admin.Username, created, lastLogin})
			}

			return renderTable([]string{"ID", "Username", colCreated, "Last login"}, rows)
		})
	}),
}

var createWebAdminCmd = &cobra.Command{
	Use:     "create USERNAME",
	Short:   "Create a web admin account",
	Aliases: []string{"c", cmdNew},
	Args:    cobra.ExactArgs(1),
	RunE: clientRunE(func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
		password, err := readPassword(cmd)
		if err != nil {
			return err
		}

		resp, err := client.CreateWebAdminWithResponse(ctx, clientv1.CreateWebAdminJSONRequestBody{
			Username: args[0],
			Password: password,
		})
		if err != nil {
			return fmt.Errorf("creating web admin: %w", err)
		}

		if resp.StatusCode() != http.StatusOK {
			return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
		}

		return printOutput(cmd, resp.JSON200.WebAdmin, "Web admin created")
	}),
}

var setWebAdminPasswordCmd = &cobra.Command{
	Use:   "set-password USERNAME",
	Short: "Set the password of a web admin account",
	Args:  cobra.ExactArgs(1),
	RunE: clientRunE(func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
		password, err := readPassword(cmd)
		if err != nil {
			return err
		}

		resp, err := client.SetWebAdminPasswordWithResponse(ctx, args[0], clientv1.SetWebAdminPasswordJSONRequestBody{
			Password: password,
		})
		if err != nil {
			return fmt.Errorf("setting web admin password: %w", err)
		}

		if resp.StatusCode() != http.StatusOK {
			return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
		}

		return printOutput(cmd, resp.JSON200, "Password updated")
	}),
}

var deleteWebAdminCmd = &cobra.Command{
	Use:     cmdDelete + " USERNAME",
	Short:   "Delete a web admin account",
	Aliases: []string{"remove", aliasDel},
	Args:    cobra.ExactArgs(1),
	RunE: clientRunE(func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
		resp, err := client.DeleteWebAdminWithResponse(ctx, args[0])
		if err != nil {
			return fmt.Errorf("deleting web admin: %w", err)
		}

		if resp.StatusCode() != http.StatusOK {
			return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
		}

		return printOutput(cmd, resp.JSON200, "Web admin deleted")
	}),
}

// readPassword reads the password from --password-file, prompts twice on a
// terminal, or reads one line from piped stdin. It is never taken from a flag
// value, which would leak into shell history and the process list.
func readPassword(cmd *cobra.Command) (string, error) {
	path, _ := cmd.Flags().GetString("password-file")
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading password file: %w", err)
		}

		return strings.TrimRight(string(data), "\r\n"), nil
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}

		return strings.TrimRight(line, "\r\n"), nil
	}

	fmt.Fprint(os.Stderr, "Password: ")

	first, err := term.ReadPassword(fd)

	fmt.Fprintln(os.Stderr)

	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}

	fmt.Fprint(os.Stderr, "Repeat password: ")

	second, err := term.ReadPassword(fd)

	fmt.Fprintln(os.Stderr)

	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}

	if string(first) != string(second) {
		return "", errPasswordMismatch
	}

	return string(first), nil
}
