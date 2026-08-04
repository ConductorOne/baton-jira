package connector

const attrName = "name"

var (
	resourcePageSize = 50

	// participantPageSize is used for the per-project participant search
	// (/rest/api/2/user/viewissue/search), an expensive endpoint that supports
	// pages of up to 1000. Large pages keep the request count manageable on
	// tenants with tens of thousands of users.
	participantPageSize = 1000

	memberEntitlement = "member"

	participateEntitlement = "participate"

	leadEntitlement = "lead"

	assignedEntitlement = "assigned"
)
