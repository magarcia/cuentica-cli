package catalog

import (
	_ "embed"
	"encoding/json"
)

// Operation describes one documented Cuéntica API operation.
type Operation struct {
	Resource    string   `json:"resource"`
	Action      string   `json:"action"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Summary     string   `json:"summary"`
	Query       []string `json:"query"`
	Body        bool     `json:"body"`
	Binary      bool     `json:"binary"`
	Paginated   bool     `json:"paginated"`
	Destructive bool     `json:"destructive"`
	Required    []string `json:"required"`
	Optional    []string `json:"optional"`
}

//go:embed endpoints.json
var endpointData []byte

// Operations returns an independent copy of the documented API catalog.
func Operations() []Operation {
	var document struct {
		Operations []Operation `json:"operations"`
	}
	if err := json.Unmarshal(endpointData, &document); err != nil {
		panic("decode embedded Cuéntica API catalog: " + err.Error())
	}
	return document.Operations
}
