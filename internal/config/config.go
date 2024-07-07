package config

import "time"

type Config struct {
	LogLevel string `env:"LOG_LEVEL" env-required:"true"`
	Worker   Worker
	Outline  Outline
	TG       TG
}

type Worker struct {
	NotifyExpiringInterval    time.Duration `env:"WORKER_NOTIFY_EXPIRING_INTERVAL" env-required:"true"`
	DeactivateExpiredInterval time.Duration `env:"WORKER_DEACTIVATE_EXPIRED_INTERVAL" env-required: "true"`
}

type Outline struct {
	URL         string        `env:"OUTLINE_URL" env-required:"true"`
	HTTPTimeout time.Duration `env:"OUTLINE_HTTP_TIMEOUT" env-required:"true"`
}

type TG struct {
	Verbose       bool          `env:"TG_VERBOSE"`
	PollerTimeout time.Duration `env:"TG_POLLER_TIMEOUT" env-required:"true"`
	HTTPTimeout   time.Duration `env:"TG_HTTP_TIMEOUT" env-required:"true"`

	Token string `env:"TG_TOKEN" env-required:"true"`
	Admin int64  `env:"TG_ADMIN" env-required:"true"`
}
