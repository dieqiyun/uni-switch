package gui

type modelRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	PName    string `json:"providerName"`
	Icon     string `json:"icon,omitempty"`
	Context  int    `json:"context,omitempty"` // the tokens it takes, when known
	// Efforts: the model's reasoning levels, when known — those a group's
	// member of it may be fixed at
	Efforts []string `json:"efforts,omitempty"`
	// CanFast: a group's member of it may be sent in its vendor's fast
	// mode (provider.CanFast)
	CanFast bool `json:"canFast,omitempty"`
	// OwnFast: asked of a provider the user added by its address, which may
	// or may not serve it (provider.OwnFast, #1615)
	OwnFast bool `json:"ownFast,omitempty"`
	// Images: agents are told it takes images (provider.Entry.Images); a
	// group's editor says of each member what it says of it (#756).
	// ImagesUnknown: nothing was read of it either way (imagesUnknown),
	// sent only when true
	Images        bool `json:"images,omitempty"`
	ImagesUnknown bool `json:"imagesUnknown,omitempty"`
	// Sub: a subscription account serves it (Provider.Account), so a
	// group's classifier set to it asks that subscription at every turn,
	// which the Routing page says (#1839)
	Sub bool `json:"sub,omitempty"`
	// VendorPicks: an image model whose vendor picks the model itself,
	// whichever is named (a ChatGPT account's, #1865), which Settings ›
	// Images says
	VendorPicks bool `json:"vendorPicks,omitempty"`
	// Free: it costs its subscription nothing (the vendor's word); Zero: its
	// provider lists it at $0 (provider.PricedFree). The add-a-model picker
	// says free by either (jiangguochao666 on X)
	Free bool `json:"free,omitempty"`
	Zero bool `json:"zero,omitempty"`
}
