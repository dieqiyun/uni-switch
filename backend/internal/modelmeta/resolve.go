package modelmeta

// Resolve implements provider.entryFor/WindowOf/replyLimit/ApplyImage and
// effortsKept. The profile is a projection into uni-switch's wire schema; it
// does not add unsupported capabilities the upstream catalog never declares.
func Resolve(vendor Facts, catalog *Facts, prefs Preferences, source string) Resolved {
	r := Resolved{Policy: Policy, Reference: Reference, Vendor: vendor, Catalog: catalog, Profile: map[string]any{}, Capabilities: map[string]any{}, Sources: map[string]string{}}
	context, output, images := Window(vendor.Context), vendor.Output, vendor.Images
	inferredImages := vendor.InferredImages
	efforts := clone(vendor.Efforts)
	reasoning := vendor.Reasoning || vendor.AlwaysThinks || len(efforts) > 0
	inputs, outputs := clone(vendor.Inputs), clone(vendor.Outputs)
	temperature := vendor.Temperature
	for key, known := range map[string]bool{"contextWindow": context > 0, "maxOutputTokens": output > 0, "imageInput": images != nil, "reasoningEfforts": len(efforts) > 0 || vendor.LevelsKnown} {
		if known {
			r.Sources[key] = "vendor"
		}
	}
	if catalog != nil {
		if context == 0 {
			// Decorate fills only a raw zero. A positive but unusably small live
			// window is not replaced by the provider's own row; ListedWindow
			// subsequently falls back to the shared catalog instead.
			context = catalog.Context
			if catalog.Provider != "" && (vendor.Context != 0 || context == 0) {
				context = catalog.SharedContext
			}
			if context > 0 {
				r.Sources["contextWindow"] = source
			}
		}
		if output == 0 {
			output = catalog.Output
			if output == 0 {
				output = catalog.SharedOutput
			}
			if output > 0 {
				r.Sources["maxOutputTokens"] = source
			}
		}
		if images == nil && catalog.Images != nil {
			images = catalog.Images
			inputs = clone(catalog.Inputs)
			r.Sources["imageInput"] = source
		}
		inferredImages = inferredImages || catalog.InferredImages
		if outputs == nil {
			outputs = clone(catalog.Outputs)
		}
		// Decorate takes the known provider's levels; a full Codex entry
		// restores the vendor's own levels in entryFor afterwards.
		if !vendor.Codex && (catalog.Provider != "" || len(efforts) == 0 && (len(catalog.Efforts) > 0 || catalog.LevelsKnown)) {
			efforts = clone(catalog.Efforts)
			r.Sources["reasoningEfforts"] = source
		}
		if catalog.Temperature != nil {
			temperature = catalog.Temperature
		}
		reasoning = reasoning || catalog.Reasoning || catalog.InferredReasoning || len(efforts) > 0
	}
	if prefs.Context > 0 {
		context = prefs.Context
		r.Sources["contextWindow"] = "user:model"
	} else if prefs.ProviderContext > 0 {
		context = prefs.ProviderContext
		own := Window(vendor.Context)
		if vendor.Context == 0 && catalog != nil && catalog.Provider != "" {
			own = catalog.Context
		}
		if vendor.MaxContext > own {
			own = vendor.MaxContext
		}
		if own > 0 && own < context {
			context = own
		}
		r.Sources["contextWindow"] = "user:provider"
	}
	if prefs.Output > 0 {
		output = prefs.Output
		r.Sources["maxOutputTokens"] = "user:model"
	} else if prefs.ProviderOutput > 0 {
		output = prefs.ProviderOutput
		r.Sources["maxOutputTokens"] = "user:provider"
	}
	if prefs.Vision != nil {
		images = prefs.Vision
		r.Sources["imageInput"] = "user:model"
	}
	efforts = keptEfforts(efforts, prefs.Efforts)
	if len(prefs.Efforts) > 0 {
		r.Sources["reasoningEfforts"] = "user:kept"
	}
	reasoning = reasoning || len(efforts) > 0
	// Ultra is a Codex client mode, not an additional vendor API effort.
	// entryFor adds it only to the reviewed model families already offering max.
	if !vendor.Codex {
		efforts = withUltra(vendor.ID, efforts)
	}
	if prefs.Reasonless {
		efforts = nil
		reasoning = false
		r.Sources["reasoningEfforts"] = "user:off"
	}
	if context > 0 {
		r.Profile["contextWindow"] = context
		r.Profile["maxInputTokens"] = context
	}
	if vendor.MaxContext > context {
		r.Profile["maxContextWindow"] = vendor.MaxContext
	}
	if output > 0 {
		r.Profile["maxOutputTokens"] = output
	}
	r.PublishedOutput = OutputWithin(context, output)
	if len(efforts) > 0 {
		r.Profile["reasoningEfforts"] = efforts
	} else if prefs.Reasonless || vendor.LevelsKnown || catalog != nil && catalog.LevelsKnown {
		r.Profile["reasoningEfforts"] = []string{}
	}
	if reasoning {
		r.Profile["reasoning"] = true
	} else if prefs.Reasonless || vendor.LevelsKnown || catalog != nil {
		r.Profile["reasoning"] = false
	}
	if !prefs.Reasonless && vendor.AlwaysThinks {
		r.Profile["thinkingAlwaysOn"] = true
	}
	if !prefs.Reasonless && vendor.DefaultEffort != "" {
		r.Profile["defaultEffort"] = vendor.DefaultEffort
	}
	if vendor.DynamicTools {
		r.Profile["dynamicTools"] = true
	}
	if temperature != nil {
		r.Profile["samplingParameters"] = *temperature
	}
	sees := inferredImages
	if images != nil {
		sees = *images
	}
	if images != nil {
		r.Capabilities["imageInput"] = *images
	} else if sees {
		r.Capabilities["imageInput"] = true
		r.Sources["imageInput"] = source + ":inferred"
	}
	// Entry.Modalities: preserve audio/video/pdf, and let vision override
	// the image member without erasing the other advertised input kinds.
	if inputs != nil {
		filtered := []string{}
		for _, kind := range inputs {
			if kind != "image" {
				filtered = append(filtered, kind)
			}
		}
		inputs = filtered
	}
	if sees {
		if inputs == nil {
			inputs = []string{"text", "image"}
		} else {
			at := 0
			if len(inputs) > 0 && inputs[0] == "text" {
				at = 1
			}
			inputs = append(inputs, "")
			copy(inputs[at+1:], inputs[at:])
			inputs[at] = "image"
		}
	} else if inputs == nil && images != nil {
		inputs = []string{"text"}
	}
	if inputs != nil {
		r.Profile["inputModalities"] = inputs
	}
	if outputs != nil {
		r.Profile["outputModalities"] = outputs
	}
	if len(vendor.APIs) > 0 {
		r.Profile["endpoints"] = map[string]any{"messages": includes(vendor.APIs, "anthropic"), "chatCompletions": includes(vendor.APIs, "chat"), "responses": includes(vendor.APIs, "responses")}
	}
	r.EffortMapping = map[string]string{}
	for _, want := range append(append(clone(effortOrder), "light", "extra_high"), efforts...) {
		r.EffortMapping[want] = NearestEffort(want, efforts)
	}
	return r
}
