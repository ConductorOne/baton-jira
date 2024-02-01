package main

import (
	"context"
	"errors"

	"github.com/conductorone/baton-sdk/pkg/cli"
	"github.com/spf13/cobra"
)

// config defines the external configuration required for the connector to run.
type config struct {
	cli.BaseConfig `mapstructure:",squash"` // Puts the base config options in the same place as the connector options

	JiraUrl  string `mapstructure:"jira-url"`
	Email    string `mapstructure:"jira-email"`
	ApiToken string `mapstructure:"jira-api-token"`
}

// validateConfig is run after the configuration is loaded, and should return an error if it isn't valid.
func validateConfig(ctx context.Context, cfg *config) error {
	if cfg.Email == "" || cfg.ApiToken == "" {
		return errors.New("jira-connector: BasicAuth options must be provided")
	}

	if cfg.JiraUrl == "" {
		return errors.New("jira-connector: jira-url must be provided")
	}

	return nil
}

// cmdFlags sets the cmdFlags required for the connector.
func cmdFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String("jira-url", "", "Url to Jira service. ($BATON_JIRA_URL)")
	cmd.PersistentFlags().String("jira-email", "", "Email for Jira service. ($BATON_JIRA_EMAIL)")
	cmd.PersistentFlags().String("jira-api-token", "", "API token for Jira service. ($BATON_JIRA_API_TOKEN)")
}
