package main

import (
	cfg "github.com/conductorone/baton-jira/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/config"
)

func main() {
	config.Generate("jira", cfg.Config)
}
