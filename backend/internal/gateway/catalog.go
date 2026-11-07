package gateway

type CatalogInput struct {
	Models       []Model `json:"models"`
	DefaultModel string  `json:"defaultModel"`
	Requested    *string `json:"requested,omitempty"`
}
type ModelRoute struct {
	ID                 string `json:"id"`
	DesktopID          string `json:"desktopId"`
	LegacyID           string `json:"legacyId"`
	AlternateDesktopID string `json:"alternateDesktopId"`
}

func Catalog(input CatalogInput) (Object, error) {
	c := Call{Models: input.Models, DefaultModel: input.DefaultModel}
	rows := []ModelRoute{}
	for _, m := range c.enabledModels() {
		rows = append(rows, ModelRoute{ID: m.ID, DesktopID: DesktopID(m), LegacyID: LegacyAlias(m.ID), AlternateDesktopID: alternateDesktopID(m)})
	}
	out := Object{"routes": rows}
	if input.Requested != nil {
		m, e := c.resolveModel(*input.Requested)
		if e != nil {
			return nil, e
		}
		out["model"] = m.ID
	}
	return out, nil
}
