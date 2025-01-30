package main

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	jiraUrlField     = field.StringField("jira-url", field.WithRequired(true), field.WithDescription("Url to Jira service."))
	emailField       = field.StringField("jira-email", field.WithRequired(true), field.WithDescription("Email for Jira service."))
	apiTokenField    = field.StringField("jira-api-token", field.WithRequired(true), field.WithDescription("API token for Jira service."))
	projectKeysField = field.StringSliceField("jira-project-keys", field.WithDescription("Comma-separated list of Jira project keys to use for tickets."))
	skipProjectsField = field.BoolField("skip-projects", field.WithRequired(false), field.WithDescription("Skip syncing projects."))
)

var configurationFields = []field.SchemaField{
	jiraUrlField,
	emailField,
	apiTokenField,
	projectKeysField,
	skipProjectsField,
}
