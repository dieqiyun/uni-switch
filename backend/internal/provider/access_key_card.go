package provider

// accessKeyCard is the card of p's plan, whose usage is read with the
// account's access key, while no provider on the plan has one (#1821):
// the card was left out with nothing said, and a key lost from
// providers.json looked like a card gone for no reason. Its error names
// the fields as the vendor's console does; NeedsAccessKey has the Usage
// page offer the provider's settings.
func accessKeyCard(p Provider, plan string) SubscriptionQuota {
	return SubscriptionQuota{Provider: p.ID, Name: p.Name, Icon: p.Icon, Plan: plan, Windows: []QuotaWindow{},
		Error: noAccessKeyError(p), NeedsAccessKey: true}
}

// noAccessKeyError is why a plan's card has no windows when p has no
// access key: what to give, and where.
func noAccessKeyError(p Provider) string {
	fields := "AccessKey ID and Secret"
	if AccessKeyVendor(p) == "tencent" {
		fields = "SecretId and SecretKey"
	}
	return "this plan's usage is read with the account's access key: give its " + fields + " in " + p.Name + "'s settings"
}
