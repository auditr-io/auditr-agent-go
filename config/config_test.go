package config

import (
	"log"
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestSeedConfig(t *testing.T) {
	// Load config file to compare later
	configFile, ok := os.LookupEnv("CONFIG")
	if !ok {
		log.Fatal("CONFIG env var not set")
	}

	v := viper.New()
	v.SetConfigType("env")
	v.SetConfigFile(configFile)
	if err := v.ReadInConfig(); err != nil {
		log.Fatalf("Failed reading config file %s\n", configFile)
	}

	tests := map[string]struct {
		getVar func() interface{}
		value  interface{}
	}{
		"ConfigURL": {
			getVar: func() interface{} { return v.GetString("auditr_config_url") },
			value:  ConfigURL,
		},
		"AuthURL": {
			getVar: func() interface{} { return v.GetString("auditr_auth_url") },
			value:  AuthURL,
		},
		"ClientID": {
			getVar: func() interface{} { return v.GetString("auditr_client_id") },
			value:  ClientID,
		},
		"ClientSecret": {
			getVar: func() interface{} { return v.GetString("auditr_client_secret") },
			value:  ClientSecret,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.getVar(), tc.value)
		})
	}
}

// func TestBaseURL(t *testing.T) {
// 	assert.NotEmpty(t, BaseURL, "BaseURL is empty")
// }

// func TestEventsURL(t *testing.T) {
// 	assert.NotEmpty(t, EventsURL, "EventsURL is empty")
// }

// func TestTargetRoutes(t *testing.T) {
// 	assert.NotEmpty(t, TargetRoutes, "TargetRoutes is empty")
// }

// func TestSampledRoutes(t *testing.T) {
// 	assert.NotEmpty(t, SampledRoutes, "TargetRoutes is empty")
// }
