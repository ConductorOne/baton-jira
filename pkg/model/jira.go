package model

// https://developer.atlassian.com/platform/forge/manifest-reference/modules/jira-custom-field-type/#data-types
const (
	TypeString   = "string"
	TypeArray    = "array"
	TypeDate     = "date"
	TypeDateTime = "datetime"
	TypeNumber   = "number"
	TypeUser     = "user"
	TypeGroup    = "group"
	TypeObject   = "object"
	TypeOption   = "option"
)

type MetaDataFields struct {
	Required        bool     `json:"required"`
	Schema          Schema   `json:"schema"`
	Name            string   `json:"name"`
	Key             string   `json:"key"`
	HasDefaultValue bool     `json:"hasDefaultValue"`
	AllowedValues   []Choice `json:"allowedValues,omitempty"`
}

type Choice struct {
	Id string `json:"id"`
	// Could be name or value in most cases
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

type Schema struct {
	Type     string `json:"type"`
	Custom   string `json:"custom"`
	CustomId int    `json:"customId"`
	Items    string `json:"items,omitempty"`
}
