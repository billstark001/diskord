package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"diskord/internal/app"
	"diskord/internal/ca"
	"diskord/internal/config"
	"diskord/internal/securefs"
	"diskord/internal/ui"
	"github.com/spf13/cobra"
)

const version = "0.1.0-prototype"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "diskord:", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	configPath := "diskord.yaml"
	root := &cobra.Command{
		Use: "diskord", Version: version,
		Short:         "Local, explicitly configured Discord traffic observer",
		Long:          "Observe authorized local Discord traffic. Root CA trust and client proxy settings are always explicit.",
		SilenceErrors: true, SilenceUsage: true,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runProxy(configPath) },
	}
	root.PersistentFlags().StringVar(&configPath, "config", configPath, "configuration file")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%s: %w (run %s --help)", cmd.CommandPath(), err, cmd.CommandPath())
	})

	var runtimeDir, openssl string
	initCmd := &cobra.Command{Use: "init", Short: "Initialize a runtime directory", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return initialize(configPath, runtimeDir, openssl) }}
	initCmd.Flags().StringVar(&runtimeDir, "runtime-dir", ".", "runtime directory")
	initCmd.Flags().StringVar(&openssl, "openssl", "openssl", "OpenSSL executable")
	root.AddCommand(initCmd)

	caCmd := &cobra.Command{Use: "ca", Short: "Issue or select a root CA", RunE: func(*cobra.Command, []string) error { return errors.New("choose ca issue or ca select") }}
	for _, operation := range []string{"issue", "select"} {
		operation := operation
		var certPath, keyPath, tool string
		command := &cobra.Command{Use: operation, Short: operation + " a root CA", Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				if certPath == "" || keyPath == "" {
					return errors.New("explicit --cert and --key are required")
				}
				manager, err := config.New(configPath)
				if err != nil {
					return err
				}
				lock, err := securefs.Acquire(manager.Root)
				if err != nil {
					return fmt.Errorf("%w; while running, use the authenticated web controls", err)
				}
				defer lock.Close()
				if operation == "issue" {
					if tool == "" {
						tool = manager.Current().CA.OpenSSL
					}
					if err := ca.Issue(context.Background(), manager.Root, certPath, keyPath, tool); err != nil {
						return err
					}
					authority, err := ca.Load(manager.Root, certPath, keyPath)
					if err != nil {
						return err
					}
					fmt.Printf("CA issued, not selected or trusted.\nCertificate: %s\nKey: %s\nSHA-256: %s\n", certPath, keyPath, authority.Fingerprint())
					return nil
				}
				var selected *ca.Authority
				_, err = manager.Patch(manager.Revision(), map[string]any{"ca.cert": certPath, "ca.key": keyPath}, func(c config.Config) error {
					var loadErr error
					selected, loadErr = ca.Load(manager.Root, c.CA.Cert, c.CA.Key)
					return loadErr
				})
				if err != nil {
					return err
				}
				fmt.Printf("CA selected. SHA-256: %s\nInstall only the public certificate manually, never the private key.\n", selected.Fingerprint())
				if !selected.TrustedForTLS() {
					fmt.Println(ca.TrustInstructions())
				}
				return nil
			}}
		command.Flags().StringVar(&certPath, "cert", "", "certificate path under runtime")
		command.Flags().StringVar(&keyPath, "key", "", "private key path under runtime")
		if operation == "issue" {
			command.Flags().StringVar(&tool, "openssl", "", "OpenSSL executable override")
		}
		caCmd.AddCommand(command)
	}
	root.AddCommand(caCmd)

	configCmd := &cobra.Command{Use: "config", Short: "Manage configuration", RunE: func(*cobra.Command, []string) error { return errors.New("choose config set") }}
	configCmd.AddCommand(&cobra.Command{Use: "set FIELD true|false", Short: "Set a supported boolean setting", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			field := args[0]
			switch field {
			case "resources.enabled", "logging.file.enabled", "logging.discord.enabled":
			default:
				return fmt.Errorf("unsupported setting %q", field)
			}
			enabled, err := strconv.ParseBool(args[1])
			if err != nil {
				return fmt.Errorf("%s: expected true or false", field)
			}
			manager, err := config.New(configPath)
			if err != nil {
				return err
			}
			lock, err := securefs.Acquire(manager.Root)
			if err != nil {
				return fmt.Errorf("%w; while running, use the authenticated web controls", err)
			}
			defer lock.Close()
			if _, err := manager.Patch(manager.Revision(), map[string]any{field: enabled}, nil); err != nil {
				return err
			}
			fmt.Println("Configuration updated.")
			return nil
		}})
	root.AddCommand(configCmd)

	var discordPath string
	discordCmd := &cobra.Command{Use: "discord", Short: "Desktop client helpers", RunE: func(*cobra.Command, []string) error { return errors.New("choose discord launch") }}
	launchCmd := &cobra.Command{Use: "launch", Short: "Launch Discord using the configured proxy", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			manager, err := config.New(configPath)
			if err != nil {
				return err
			}
			return launchDiscord(manager, discordPath)
		}}
	launchCmd.Flags().StringVar(&discordPath, "path", "", "Discord executable path")
	discordCmd.AddCommand(launchCmd)
	root.AddCommand(discordCmd)

	root.AddCommand(&cobra.Command{Use: "doctor", Short: "Check local configuration and CA", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			manager, err := config.New(configPath)
			if err != nil {
				return err
			}
			return doctor(manager)
		}})
	root.AddCommand(&cobra.Command{Use: "ui-token", Short: "Print the local console access token", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			manager, err := config.New(configPath)
			if err != nil {
				return err
			}
			token, err := ui.Token(manager.Root)
			if err != nil {
				return err
			}
			fmt.Println(token)
			return nil
		}})
	root.AddCommand(&cobra.Command{Use: "run", Short: "Start the proxy and console", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runProxy(configPath) }})
	root.AddCommand(&cobra.Command{Use: "run-bg", Short: "Start diskord in the current working directory in the background", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runBackground(configPath) }})
	root.AddCommand(&cobra.Command{Use: "stop-bg", Short: "Stop the background diskord in the current working directory", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return stopBackground(configPath) }})
	root.AddCommand(&cobra.Command{Use: "version", Short: "Print diskord version", Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) { cmd.Println(version) }})
	return root
}

func runProxy(configPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.Run(ctx, configPath)
}
