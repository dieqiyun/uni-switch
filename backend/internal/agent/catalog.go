package agent

import (
	_ "embed"
	"encoding/json"
)

// Client describes a configuration contract, rather than a request User-Agent.
// See docs/magpie-clients-reference.json for the pinned upstream review.
type Client struct {
	Mode        string   `json:"mode"`
	Description string   `json:"description"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Protocols   []string `json:"protocols"`
}

//go:embed client-catalog.json
var clientCatalog []byte
var ExtraClients = func() []Client {
	var clients []Client
	if err := json.Unmarshal(clientCatalog, &clients); err != nil {
		panic(err)
	}
	return clients
}()

func LookupClient(id string) (Client, bool) {
	for _, c := range ExtraClients {
		if c.ID == id {
			return c, true
		}
	}
	return Client{}, false
}
