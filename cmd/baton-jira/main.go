package main

import (
	"context"
	"fmt"
	"os"

	cfg "github.com/conductorone/baton-jira/pkg/config"
	"github.com/conductorone/baton-jira/pkg/connector"
	configSchema "github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()

	_, cmd, err := configSchema.DefineConfiguration(ctx, "baton-jira", getConnector, cfg.Config)
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

func getConnector(ctx context.Context, jc *cfg.Jira) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)

	projectKeys := jc.JiraProjectKeys

	builder := connector.JiraBasicAuthBuilder{
		Base: &connector.JiraOptions{
			Url:         jc.JiraUrl,
			ProjectKeys: projectKeys,
		},
		Username: jc.JiraEmail,
		ApiToken: jc.JiraApiToken,
	}

	if jc.AtlassianOrgid != "" && jc.AtlassianApiToken != "" {
		builder.Base.AtlassianBuilder = &connector.AtlassianAuthBuilder{
			OrganizationId: jc.AtlassianOrgid,
			AccessToken:    jc.AtlassianApiToken,
		}
	}

	jiraConnector, err := builder.New(ctx, jc.SkipProjectParticipants, jc.SkipCustomerUser)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}

	opts := make([]connectorbuilder.Opt, 0)
	if jc.Ticketing {
		opts = append(opts, connectorbuilder.WithTicketingEnabled())
	}

	c, err := connectorbuilder.NewConnector(ctx, jiraConnector, opts...)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}

	return c, nil
}
