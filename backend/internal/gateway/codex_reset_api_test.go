package gateway

// #3850 (prgrmrwy): uni-switch runs on a home server, and the office's remote
// uni-switch card (or DSH's, reading the cards) said "1 reset" with no way to
// use it but walking to the server. POST /v1/uni-switch/quotas/codex-reset
// spends it there, through the Usage page's own code. A reset can't be
// undone, so only a gateway key with full access spends one: never a
// request with no key, from this computer or a page or app the gateway
// lets in on loopback without one, nor a read-only key or a held one.

// A local full key may reset the named account once.

// none left: said so, nothing more spent

// The office's uni-switch spends the home server's reset through its remote
// uni-switch provider (#3850): the card here keeps the remote's resets, and
// using one asks the remote with the provider's key. A read-only key there
// says so in words the card can show.

// the remote's card, read there, carries its reset here

// read again after: the card shows none left
