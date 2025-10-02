package main

import (
	cfg "github.com/conductorone/baton-jira/pkg/config"
	"github.com/conductorone/baton-jira/pkg/connector"
	configSchema "github.com/conductorone/baton-sdk/pkg/config"
)

var version = "dev"

func main() {
	configSchema.RunConnector("baton-jira", version, cfg.Config, connector.New)
}
