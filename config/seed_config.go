package config

import (
	"log"
	"os"
	"sync"

	"github.com/spf13/viper"
)

var (
	ConfigURL string = "https://config.auditr.io"
	APIKey    string

	seedOnce sync.Once
)

func ensureSeedConfig() {
	seedOnce.Do(func() {
		viper.SetConfigType("env")
		viper.BindEnv("auditr_config_url")
		viper.BindEnv("auditr_api_key")

		// If an env vars file is available, load the env vars in it
		if configFile, ok := os.LookupEnv("ENV_PATH"); ok {
			viper.SetConfigFile(configFile)

			if err := viper.ReadInConfig(); err != nil {
				log.Printf("Error reading env vars file: %v\n", err)
			}
		}

		ConfigURL = viper.GetString("auditr_config_url")
		APIKey = viper.GetString("auditr_api_key")
		if APIKey == "" {
			log.Fatalf("AUDITR_API_KEY is not set")
		}
	})
}
