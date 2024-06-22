package config

type Config struct {
	LogLevel   string `env:"LOG_LEVEL" env-required:"true"`
	OutlineURL string `env:"OUTLINE_URL" env-required:"true"`
	TGToken    string `env:"TG_TOKEN" env-required:"true"`
	TGAdmin    int64  `env:"TG_ADMIN" env-required:"true"`
}
