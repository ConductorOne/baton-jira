package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

func init() {
	ticketField 		:= field.TicketingField
	externalTicketField := &ticketField

	externalTicketField.ExportTarget = field.ExportTargetGUI
	Config = field.NewConfiguration([]field.SchemaField{
		jiraUrlField,
		emailField,
		apiTokenField,
		projectKeysField,
		skipProjectParticipantsField,
		*externalTicketField,
	})
}

var (
	jiraUrlField                 = field.StringField("jira-url", field.WithRequired(true), field.WithDescription("Url to Jira service."))
	emailField                   = field.StringField("jira-email", field.WithRequired(true), field.WithDescription("Email for Jira service."))
	apiTokenField                = field.StringField("jira-api-token", field.WithRequired(true), field.WithDescription("API token for Jira service."))
	projectKeysField             = field.StringSliceField("jira-project-keys", field.WithDescription("Comma-separated list of Jira project keys to use for tickets."))
	skipProjectParticipantsField = field.BoolField("skip-project-participants", field.WithRequired(false), field.WithDescription("Skip syncing project participants."))
)

//go:generate go run ./gen
var Config field.Configuration
