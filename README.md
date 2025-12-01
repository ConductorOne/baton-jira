![Baton Logo](./docs/images/baton-logo.png)

# `baton-jira` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-jira.svg)](https://pkg.go.dev/github.com/conductorone/baton-jira) ![main ci](https://github.com/conductorone/baton-jira/actions/workflows/main.yaml/badge.svg)

`baton-jira` is a connector for Jira built using the [Baton SDK](https://github.com/conductorone/baton-sdk). It works with both cloud or on-premise Jira installations through the Jira V3 API. This connector helps organizations manage and review access to Jira resources through automated discovery and provisioning capabilities.

## Connector Capabilities

This connector provides the following capabilities:

- **Sync**: Discovers and syncs all users, groups, projects, and project roles from your Jira instance
- **Provisioning**: Grants and revokes access to Jira groups and project roles
- **Ticketing**: Creates and tracks Jira tickets for access requests/approvals workflows

## Resource Types Synced

The connector syncs the following Jira resources:

- **Users**: All Jira user accounts (human and service accounts)
- **Groups**: Jira groups and their memberships
- **Projects**: All Jira projects (or filtered by project keys if specified)
- **Project Roles**: Role-based permissions within projects (e.g., Administrators, Developers)

## Value Provided

- **Centralized Access Management**: Manage Jira access alongside other systems
- **Automated Access Reviews**: Easily review who has access to which Jira projects and roles
- **Streamlined Provisioning**: Grant or revoke access to groups and project roles
- **Access Request Workflows**: Integrate with ticketing workflows for access approvals

Check out [Baton](https://github.com/conductorone/baton) to learn more about the project in general.

# Prerequisites

This connector uses Jira Basic Auth, requiring an email address and API token. The connector supports both **regular user accounts** and **Atlassian service accounts** (emails look like `<my-service-account>@serviceaccount.atlassian.com`). To use this connector, you need:

1. **Appropriate Permissions**: The account used needs permissions for the operations below (admin privileges provide all these, but more granular permissions can work too):
   - View users, groups, and projects
   - View project roles
   - Manage group memberships (for provisioning)
   - Manage project role memberships (for provisioning)
   - Create issues (for ticketing)
2. **API Token**: A personal API token for authentication (see below for creation steps)
3. **Jira URL**: The URL of your Jira instance
4. **Email Address**: The email associated with your Jira account

## Creating an API Token

1. Log in to your Atlassian account at [id.atlassian.com](https://id.atlassian.com/)
2. Navigate to **Security** in the left sidebar
3. Under **API tokens**, click **Create and manage API tokens**
4. Click **Create API token**
5. Give your token a meaningful label (e.g., "Baton Access Management")
6. Copy and securely store the generated token - it will only be shown once

For detailed instructions, see the [Atlassian documentation on API tokens](https://developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/).

## Configuration Options

The connector requires the following configuration:

| Parameter | Description | Required | Environment Variable | Flag |
|-----------|-------------|----------|----------------------|------|
| Jira URL | URL of your Jira instance | Yes | `BATON_JIRA_URL` | `--jira-url` |
| Email | Email address for Jira authentication | Yes | `BATON_JIRA_EMAIL` | `--jira-email` |
| API Token | API token for Jira authentication | Yes | `BATON_JIRA_API_TOKEN` | `--jira-api-token` |
| Project Keys | Comma-separated list of project keys to sync (optional) | No | `BATON_JIRA_PROJECT_KEYS` | `--jira-project-keys` |
| Ticketing | Enable ticketing support | No | `BATON_TICKETING` | `--ticketing` |
| Skip Project Participants | Skip syncing project participants (improves performance) | No | n/a | `--skip-project-participants` |

## Installation Methods

### Homebrew

```bash
# Install the connector
brew install conductorone/baton/baton conductorone/baton/baton-jira

# Run the connector with your configuration
BATON_JIRA_EMAIL='user@domain' BATON_JIRA_API_TOKEN='token' BATON_JIRA_URL='your-jira.atlassian.com' baton-jira
baton resources
```

### Docker

```bash
# Run the connector with your configuration
docker run --rm -v $(pwd):/out \
  -e BATON_JIRA_EMAIL='user@domain' \
  -e BATON_JIRA_API_TOKEN='token' \
  -e BATON_JIRA_URL='your-jira.atlassian.com' \
  ghcr.io/conductorone/baton-jira:latest -f "/out/sync.c1z"

# View synced resources
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

### From Source

```bash
# Install the connector
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-jira/cmd/baton-jira@main

# Run the connector with your configuration
BATON_JIRA_EMAIL='user@domain' BATON_JIRA_API_TOKEN='token' BATON_JIRA_URL='your-jira.atlassian.com' baton-jira

# View synced resources
baton resources
```

# Advanced Features

## Filtering by Project Keys

You can limit which projects are synced by specifying project keys:

```bash
BATON_JIRA_PROJECT_KEYS=PROJ1,PROJ2,PROJ3 baton-jira
```

This is useful for large Jira instances where you only need to manage access for specific projects.

## Ticketing Support

The connector can create and manage Jira tickets for access requests and approvals:

```bash
BATON_JIRA_EMAIL='user@domain' BATON_JIRA_API_TOKEN='token' BATON_JIRA_URL='your-jira.atlassian.com' BATON_TICKETING=true baton-jira
```

When ticketing is enabled, the connector will:
- Discover available issue types from your Jira projects
- Allow creation of tickets through the Baton API
- Track ticket status for access requests

## Performance Optimization

For large Jira instances, you can improve sync performance by skipping project participants:

```bash
BATON_JIRA_EMAIL='user@domain' BATON_JIRA_API_TOKEN='token' BATON_JIRA_URL='your-jira.atlassian.com' --skip-project-participants baton-jira
```

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
  config             Get the connector config schema
  help               Help about any command

Flags:
      --atlassian-api-token string                       api token to atlassian organization ($BATON_ATLASSIAN_API_TOKEN)
      --atlassian-orgId string                           organization Id to atlassian instance ($BATON_ATLASSIAN_ORGID)
      --client-id string                                 The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string                             The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
      --external-resource-c1z string                     The path to the c1z file to sync external baton resources with ($BATON_EXTERNAL_RESOURCE_C1Z)
      --external-resource-entitlement-id-filter string   The entitlement that external users, groups must have access to sync external baton resources ($BATON_EXTERNAL_RESOURCE_ENTITLEMENT_ID_FILTER)
  -f, --file string                                      The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                                             help for baton-jira
      --jira-api-token string                            required: API token for Jira service. ($BATON_JIRA_API_TOKEN)
      --jira-email string                                required: Email for Jira service. ($BATON_JIRA_EMAIL)
      --jira-project-keys strings                        Comma-separated list of Jira project keys to use for tickets. ($BATON_JIRA_PROJECT_KEYS)
      --jira-url string                                  required: Url to Jira service. ($BATON_JIRA_URL)
      --log-format string                                The output format for logs: json, console ($BATON_LOG_FORMAT) (default "console")
      --log-level string                                 The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
      --log-level-debug-expires-at string                The timestamp indicating when debug-level logging should expire ($BATON_LOG_LEVEL_DEBUG_EXPIRES_AT)
      --otel-collector-endpoint string                   The endpoint of the OpenTelemetry collector to send observability data to (used for both tracing and logging if specific endpoints are not provided) ($BATON_OTEL_COLLECTOR_ENDPOINT)
  -p, --provisioning                                     This must be set in order for provisioning actions to be enabled ($BATON_PROVISIONING)
      --skip-customer-user                               Skip syncing customer users in Jira Service Management. ($BATON_SKIP_CUSTOMER_USER)
      --skip-full-sync                                   This must be set to skip a full sync ($BATON_SKIP_FULL_SYNC)
      --skip-project-participants                        Skip syncing project participants. ($BATON_SKIP_PROJECT_PARTICIPANTS)
      --sync-resources strings                           The resource IDs to sync ($BATON_SYNC_RESOURCES)
      --ticketing                                        This must be set to enable ticketing support ($BATON_TICKETING)
  -v, --version                                          version for baton-jira

Use "baton-jira [command] --help" for more information about a command.
```
