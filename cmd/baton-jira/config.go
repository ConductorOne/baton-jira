package main

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	jiraUrlField  = field.StringField("jira-url", field.WithRequired(true), field.WithDescription("Url to Jira service. ($BATON_JIRA_URL)"))
	emailField    = field.StringField("jira-email", field.WithRequired(true), field.WithDescription("Email for Jira service. ($BATON_JIRA_EMAIL)"))
	apiTokenField = field.StringField("jira-api-token", field.WithRequired(true), field.WithDescription("API token for Jira service. ($BATON_JIRA_API_TOKEN)"))
)

var configurationFields = []field.SchemaField{
	jiraUrlField,
	emailField,
	apiTokenField,
}
