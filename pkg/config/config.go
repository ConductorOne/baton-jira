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

	skipCustomerUser = field.BoolField(
		"skip-customer-user",
		field.WithDisplayName("Skip customer user"),
		field.WithDescription("Skip syncing customer users in Jira Service Management."),
		field.WithDefaultValue(false),
	)

	atlassianOrgId = field.StringField(
		"atlassian-orgId",
		field.WithDisplayName("Atlassian organization Id"),
		field.WithDescription("organization Id to atlassian instance"),
	)
	atlassianAPIToken = field.StringField(
		"atlassian-api-token",
		field.WithDisplayName("Atlassian organization api token"),
		field.WithDescription("api token to atlassian organization"),
		field.WithIsSecret(true),
	)
)

//go:generate go run ./gen
var Config = field.NewConfiguration(
	[]field.SchemaField{
		jiraUrlField,
		emailField,
		apiTokenField,
		projectKeysField,
		skipProjectParticipantsField,
		externalTicketField,
		skipCustomerUser,
		atlassianOrgId,
		atlassianAPIToken,
	},
	field.WithConnectorDisplayName("Jira Cloud"),
	field.WithHelpUrl("/docs/baton/jira-cloud"),
	field.WithIconUrl("/static/app-icons/jira.svg"),
)
