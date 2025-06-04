package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	jiraUrlField = field.StringField(
		"jira-url",
		field.WithDisplayName("Jira site domain"),
		field.WithRequired(true),
		field.WithDescription("Url to Jira service."),
	)
	emailField = field.StringField(
		"jira-email",
		field.WithDisplayName("Your Jira email address"),
		field.WithRequired(true),
		field.WithDescription("Email for Jira service."),
	)
	apiTokenField = field.StringField(
		"jira-api-token",
		field.WithDisplayName("API token"),
		field.WithRequired(true),
		field.WithDescription("API token for Jira service."),
		field.WithIsSecret(true),
	)
	projectKeysField = field.StringSliceField(
		"jira-project-keys",
		field.WithDisplayName("Project keys"),
		field.WithDescription("Comma-separated list of Jira project keys to use for tickets."),
	)
	skipProjectParticipantsField = field.BoolField(
		"skip-project-participants",
		field.WithDisplayName("Skip project participants"),
		field.WithDescription("Skip syncing project participants."),
	)
	externalTicketField = field.TicketingField.ExportAs(field.ExportTargetGUI)
)

//go:generate go run ./gen
var Config = field.NewConfiguration(
	[]field.SchemaField{jiraUrlField, emailField, apiTokenField, projectKeysField, skipProjectParticipantsField, externalTicketField},
	field.WithConnectorDisplayName("Jira Cloud"),
	field.WithHelpUrl("/docs/baton/jira-cloud"),
	field.WithIconUrl("/static/app-icons/jira.svg"),
)
