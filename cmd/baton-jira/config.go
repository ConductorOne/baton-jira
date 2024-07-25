package main

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	jiraUrlField  = field.StringField("jira-url", field.WithRequired(true), field.WithDescription("The URL that Jira is hosted at. Example: https://localhost:8080"))
	emailField    = field.StringField("jira-email", field.WithRequired(true), field.WithDescription("The email address to authenticate with."))
	apiTokenField = field.StringField("jira-api-token", field.WithRequired(true), field.WithDescription("The Jira personal access token to authenticate with."))
)

var configurationFields = []field.SchemaField{
	jiraUrlField,
	emailField,
	apiTokenField,
}
