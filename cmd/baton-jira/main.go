package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/conductorone/baton-jira/pkg/connector"
	configSchema "github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()

	_, cmd, err := configSchema.DefineConfiguration(ctx, "baton-jira", getConnector, field.NewConfiguration(configurationFields))
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	cmd.Version = version

	err = cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func getConnector(ctx context.Context, v *viper.Viper) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)

	var projectKeys []string
	if projKeysStr := v.GetString("jira-project-keys"); projKeysStr != "" {
		projectKeys = strings.Split(projKeysStr, ",")
		for i := range projectKeys {
			projectKeys[i] = strings.TrimSpace(projectKeys[i])
		}
	}

	builder := connector.JiraBasicAuthBuilder{
		Base: &connector.JiraOptions{
			Url:         v.GetString("jira-url"),
			ProjectKeys: projectKeys,
		},
		Username: v.GetString("jira-email"),
		ApiToken: v.GetString("jira-api-token"),
	}

	jiraConnector, err := builder.New()
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}

	opts := make([]connectorbuilder.Opt, 0)
	if v.GetBool(field.TicketingField.FieldName) {
		opts = append(opts, connectorbuilder.WithTicketingEnabled())
	}

	c, err := connectorbuilder.NewConnector(ctx, jiraConnector, opts...)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}

	return c, nil
}
