![Baton Logo](./docs/images/baton-logo.png)

# `baton-jira` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-jira.svg)](https://pkg.go.dev/github.com/conductorone/baton-jira) ![main ci](https://github.com/conductorone/baton-jira/actions/workflows/main.yaml/badge.svg)

`baton-jira` is a connector for Jira built using the [Baton SDK](https://github.com/conductorone/baton-sdk). It works with both cloud or on-premise Jira V3 API.

Check out [Baton](https://github.com/conductorone/baton) to learn more about the project in general.

# Prerequisites

This connector supports Jira Basic Auth. It requires an email address and api token to exchange for access token that later used throughout the communication with API. To obtain these credentials, you have to create API client in Jira. To do that you must have administrator role (more info on creating credentials [here](https://developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/)). 

After you have obtained an API token, you can use them with the connector. You can do this by setting `BATON_JIRA_EMAIL` and `BATON_JIRA_API_TOKEN` environment variables or by passing them as flags to baton-jira command.

# Getting Started

Along with credentials, you must specify Jira URL that you want to use. You can change this by setting `BATON_JIRA_URL` environment variable or by passing `--jira-url` flag to `baton-jira` command.

## brew

```
brew install conductorone/baton/baton conductorone/baton/baton-jira

BATON_JIRA_EMAIL='user@domain' BATON_JIRA_API_TOKEN=token BATON_JIRA_URL=your-jira.atlassian.com baton-jira
baton resources
```

## docker

```
docker run --rm -v $(pwd):/out -e BATON_JIRA_EMAIL='user@domain' BATON_JIRA_API_TOKEN=token BATON_JIRA_URL=your-jira.atlassian.com ghcr.io/conductorone/baton-jira:latest -f "/out/sync.c1z"
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

## source

```
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-jira/cmd/baton-jira@main

BATON_CLIENT_ID=client_id BATON_CLIENT_SECRET=client_secret baton-jira
baton resources
```

# Data Model

`baton-jira` will fetch information about the following Jira resources:

- Users
- Groups
- Projects
- Roles

# Contributing, Support and Issues

We started Baton because we were tired of taking screenshots and manually building spreadsheets. We welcome contributions, and ideas, no matter how small -- our goal is to make identity and permissions sprawl less painful for everyone. If you have questions, problems, or ideas: Please open a Github Issue!

See [CONTRIBUTING.md](https://github.com/ConductorOne/baton/blob/main/CONTRIBUTING.md) for more details.

# `baton-jira` Command Line Usage

```
baton-jira

Usage:
  baton-jira [flags]
  baton-jira [command]

Available Commands:
  capabilities       Get connector capabilities
  completion         Generate the autocompletion script for the specified shell
  help               Help about any command

Flags:
      --client-id string        The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string    The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
  -f, --file string             The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                    help for baton-jira
      --jira-api-token string   API token for Jira service. ($BATON_JIRA_API_TOKEN)
      --jira-url string         Url to Jira service. ($BATON_JIRA_URL)
      --jira-email string       Email for Jira service. ($BATON_JIRA_EMAIL)
      --log-format string       The output format for logs: json, console ($BATON_LOG_FORMAT) (default "json")
      --log-level string        The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
  -p, --provisioning            This must be set in order for provisioning actions to be enabled. ($BATON_PROVISIONING)
  -v, --version                 version for baton-jira

Use "baton-jira [command] --help" for more information about a command.
```