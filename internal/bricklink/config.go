package bricklink

import (
	"strconv"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// FromConfig builds a client from the settings file and environment (the four
// BRICKLINK_* values, plus currency, region and the daily budget), sharing store's
// cache and budget. BRICKLINK_BASE_URL exists so tests can point it at a fake.
func FromConfig(store Store) *Client {
	c := New(Credentials{
		ConsumerKey:    config.Get(config.BricklinkConsumerKey),
		ConsumerSecret: config.Get(config.BricklinkConsumerSecret),
		Token:          config.Get(config.BricklinkToken),
		TokenSecret:    config.Get(config.BricklinkTokenSecret),
	}, store)
	c.Currency = config.Get(config.BricklinkCurrency)
	c.Region = config.Get(config.BricklinkRegion)
	if n, err := strconv.Atoi(config.Get(config.BricklinkDailyBudget)); err == nil && n > 0 && n <= 5000 {
		c.DailyBudget = n // never above BrickLink's own 5,000
	}
	if base := config.Env("BRICKLINK_BASE_URL", ""); base != "" {
		c.BaseURL = base
	}
	return c
}
