package domain

import "time"

const (
	KeyTTL                = 24 * time.Hour * 30 // 30 days
	PricePerKey           = 150
	MaxKeysPerUser        = 10
	BeforeOrderExpiration = time.Hour * 24 * 3 // 3 days
)
