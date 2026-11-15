package gateway

import "github.com/dieqiyun/uni-switch/backend/internal/provider"

// searchableModel follows the provider's native search capabilities.
func searchableModel(p provider.Provider, model string) bool {
	switch {
	case googleAccount(p), geminiKeySearches(p):
		return searchModel(model)
	case partSearcher(p):
		return searchesAlone(p, model)
	}
	return true
}
