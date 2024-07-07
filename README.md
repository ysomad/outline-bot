# How to run
See `Makefile`

# Environment variables
- OUTLINE_URL - url to selfhosted outline API instance
- TG_TOKEN - access token for telegram bot api
- TG_ADMIN - telegram user id of admin, which will receive notifications
- TG_VERBOSE - debug mode for telegram api

# Dependencies

## Development
- pplog (human-readable json logs in terminal)
- goose (migrations)
- FiloSottile/musl-cross/musl-cross (for cross compilation from arm to linux amd64)
- gcc

## Remote host
- gcc, musl (for sqlite, musl for crosscompile from macos)
