package provider

import "context"

// RefreshAllCards reads this computer's saved account and API cards afresh.
func RefreshAllCards(ctx context.Context) {
	AskEveryClaudeUsage()
	ForgetBalances()
	forgetPlanQuotas()
	Quotas(Asked(ctx))
}
