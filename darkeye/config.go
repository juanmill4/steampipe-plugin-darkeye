package darkeye

import (
	"errors"
	"os"

	"github.com/turbot/steampipe-plugin-sdk/v6/plugin"
)

// Config is the connection configuration (config/darkeye.spc).
type Config struct {
	HIBRKey  *string `hcl:"haveibeenransom_api_key,optional"`
	BHKey    *string `hcl:"breachhouse_api_key,optional"`
	MaxPages *int    `hcl:"max_pages,optional"`
}

// Options is the validated, resolved configuration used by the client.
type Options struct {
	HIBRKey string
	BHKey   string
	// MaxPages caps the pages requested per query from Have I Been Ransomed?.
	// Zero means no cap: pagination follows the API until it reports no more results.
	MaxPages int
}

// GetConfig returns the connection config, tolerating a nil or pointer value.
func GetConfig(connection *plugin.Connection) Config {
	if connection == nil || connection.GetConfig() == nil {
		return Config{}
	}
	switch c := connection.GetConfig().(type) {
	case Config:
		return c
	case *Config:
		if c != nil {
			return *c
		}
	}
	return Config{}
}

// resolveOptions merges the connection config with the environment (a configured
// value, even an empty one, overrides its environment variable) and validates it.
// It runs once per connection, when the client is created.
func resolveOptions(c Config, getenv func(string) string) (Options, error) {
	o := Options{HIBRKey: getenv("HIBR_API_KEY"), BHKey: getenv("BREACHHOUSE_API_KEY")}
	if c.HIBRKey != nil {
		o.HIBRKey = *c.HIBRKey
	}
	if c.BHKey != nil {
		o.BHKey = *c.BHKey
	}
	if c.MaxPages != nil {
		if *c.MaxPages < 1 {
			return Options{}, errors.New("max_pages must be 1 or greater; leave it unset for no cap")
		}
		o.MaxPages = *c.MaxPages
	}
	return o, nil
}

func envOptions(c Config) (Options, error) { return resolveOptions(c, os.Getenv) }
