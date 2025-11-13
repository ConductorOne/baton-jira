package main

import (
	"context"

	cfg "github.com/conductorone/baton-jira/pkg/config"
	"github.com/conductorone/baton-jira/pkg/connector"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorrunner"
)

var version = "dev"

func main() {
	ctx := context.Background()
	config.RunConnector(ctx, "baton-jira", version, cfg.Config, connector.New, connectorrunner.WithSessionStoreEnabled())
}
