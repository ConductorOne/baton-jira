# Baton Jira Cloud - Connector Documentation

This document provides information needed to set up and use the connector.

## Connector Capabilities

### 1. What resources does the connector sync?

| Resource | Description |
|----------|-------------|
| **User** | Jira accounts. Entitlements and grants are skipped on this type; access is expressed through the group, project and project role types below. |
| **Group** | Jira groups. Members hold the `member` entitlement. |
| **Project** | Jira projects. Carries two entitlements: `participate`, held by every account with browse permission, and `lead`. |
| **Project role** | Roles scoped to a project, such as Administrators or Developers. Members hold the `assigned` entitlement. A group assigned to a project role produces a grant whose principal is that group, expandable to the group's `member` entitlement. |

Accounts reach the connector through two different sources, and the source decides which status mapping an account gets:

- **Jira REST** (`/rest/api/3/users/search`) returns a boolean `active`, so accounts from this path are only ever Enabled or Disabled. When both Atlassian organization settings are set, only `app` accounts come from this path.
- **The Atlassian organization directory** (`/admin/v2/orgs/{orgId}/directories/-/users`), used only when both optional Atlassian organization settings are configured, returns a composite `status` plus `accountStatus` and `membershipStatus`. The composite maps to the C1 resource status, the raw value is sent as the status detail, and all three land on the account profile as `status`, `account_status` and `membership_status`.

### 2. Can the connector provision any resources? If so, which ones?

Yes.

| Resource | Grant | Revoke | Create | Delete |
|----------|-------|--------|--------|--------|
| **Group membership** | ✅ Adds the account to the Jira group | ✅ Removes the account from the group | - | - |
| **Project role assignment** | ✅ Assigns the account to the project role | ✅ Removes the assignment | - | - |
| **Account** | - | - | ✅ Creates an Atlassian account and grants it the Jira products named in the request | - |

Account creation requires two field mappings, `email` and `products`. A request without `products` is rejected by the Atlassian API with an HTTP 400, which the connector surfaces as a generic "failed to create user" without the upstream message.

Account deletion, suspension and restoration are not implemented here on purpose. Those operations act on the Atlassian account across every Atlassian product rather than on Jira alone, so they belong to the Atlassian connector.

## Connector Credentials

### 1. What credentials or information are needed to set up the connector?

| Credential | Required | Description |
|------------|----------|-------------|
| **Jira site URL** (`--jira-url`) | Yes | The site URL, in `https://your-domain.atlassian.net` format; the scheme is required. A scoped token on a non service-account email takes the gateway form `https://api.atlassian.com/ex/jira/<cloud-id>` instead, which validation reports back when the site form answers 401. |
| **Jira email** (`--jira-email`) | Yes | The account the API token was created under. A `@serviceaccount.atlassian.com` address selects the service-account path. |
| **Jira API token** (`--jira-api-token`) | Yes | A classic or scoped Atlassian API token. |
| **Atlassian organization ID** (`--atlassian-orgId`) | No | Syncs accounts and groups from the Atlassian organization directory instead of Jira. Jira still supplies `app` accounts and the `atlassian-addons`, `atlassian-addons-admin` and `system-administrators` groups. |
| **Atlassian organization API token** (`--atlassian-api-token`) | No | The organization API key. Pairs with the organization ID. |

The two Atlassian organization settings are a pair: the organization directory is used only when both are non-empty, and a value in one with the other left blank is ignored without a warning.

Three further options affect what is synced: `--jira-project-keys` limits which projects are used for external ticketing, `--skip-customer-user` drops Jira Service Management customer accounts, and `--skip-project-participants` drops the `participate` entitlement from every project, not only its grants.

### 2. How are these credentials obtained?

The Jira API token comes from `id.atlassian.com` → **Security** → **API tokens**, signed in as the account whose email is configured. The page is per Atlassian account, so a token minted while signed in as a different account pairs with the wrong email and answers 401. Atlassian shows the value once.

The account needs to view users, groups, projects and project roles, and to manage group and project role memberships for provisioning. Browse Projects and Create issues are needed only for external ticketing. A weaker account fails rather than syncing short: validation lists groups and returns an error for any status other than 401, and the resource syncs return the API error instead of skipping the item.

The Atlassian organization API key is a different credential from a different console: `admin.atlassian.com` → **Settings** → **API keys** → **Create API key**, created without scopes. The organization ID is shown alongside it. It has its own expiry, so renewing the Jira token does nothing for it.

With a service account, the connector resolves the cloud ID from the configured site URL and builds the `https://api.atlassian.com/ex/jira/<cloud-id>` gateway address itself. Every other email uses the site URL as configured, so a scoped token there carries the gateway address in `--jira-url`. The gateway address is accepted, but it never equals a workspace `hostUrl`, so it cannot be combined with the organization directory settings.

## Additional Notes

### Jira plan requirements

Nothing in the connector keys off a Jira plan tier. Two settings depend on which Atlassian products a site carries: `--skip-customer-user` only matters on a site with Jira Service Management, and the Atlassian organization settings require an Atlassian organization with an admin who can mint an organization API key.

### Site resolution

The organization directory is addressed per site. The connector lists the organization's workspaces and keeps those whose `hostUrl` equals the configured site URL exactly, so the configured value has to match what Atlassian reports, scheme included and with no trailing slash. A site carrying several Atlassian products appears as one workspace per product, and the directory is then read once per matching workspace; the bundle stores each account once, so the duplication costs requests rather than data.

### API Documentation Links

- [Jira Cloud platform REST API v3](https://developer.atlassian.com/cloud/jira/platform/rest/v3/intro/)
- [Atlassian organizations REST API](https://developer.atlassian.com/cloud/admin/organization/rest/intro/)
