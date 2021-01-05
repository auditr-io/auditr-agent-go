package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/auditr-io/auditr-agent-go/auth"
	"github.com/spf13/viper"
)

// Seed configuration
var (
	// ConfigURL is the seed URL to get the rest of the configuration
	ConfigURL string

	// AuthURL is the URL to authenticate the agent
	AuthURL string

	// Client credentials
	ClientID     string
	ClientSecret string
)

// Acquired configuration
var (
	BaseURL       string
	EventsURL     string
	TargetRoutes  []string
	SampledRoutes []string
)

type config struct {
	BaseURL       string   `json:"base_url"`
	EventsPath    string   `json:"events_path"`
	TargetRoutes  []string `json:"target"`
	SampledRoutes []string `json:"sampled"`
}

func init() {
	viper.SetConfigType("env")

	viper.BindEnv("auditr_config_url")
	viper.BindEnv("auditr_auth_url")
	viper.BindEnv("auditr_client_id")
	viper.BindEnv("auditr_client_secret")

	viper.SetDefault("auditr_config_url", "https://config.auditr.io")
	viper.SetDefault("auditr_auth_url", "https://auth.auditr.io/oauth2/token")

	// If a config file is available, load the env vars in it
	if configFile, ok := os.LookupEnv("CONFIG"); ok {
		viper.SetConfigFile(configFile)

		if err := viper.ReadInConfig(); err != nil {
			log.Printf("Error reading config file: %v\n", err)

		}
	}

	ConfigURL = viper.GetString("auditr_config_url")
	AuthURL = viper.GetString("auditr_auth_url")
	ClientID = viper.GetString("auditr_client_id")
	ClientSecret = viper.GetString("auditr_client_secret")

	// cfg, err := getConfig()
	// if err != nil {
	// 	log.Fatalln("Error getting config:", err)
	// }

	// BaseURL = cfg.BaseURL
	// EventsURL = BaseURL + cfg.EventsPath

	// TargetRoutes = cfg.TargetRoutes
	// SampledRoutes = cfg.SampledRoutes
}

func getConfig() (*config, error) {
	req, err := http.NewRequest("GET", ConfigURL, nil)
	if err != nil {
		log.Println("Error http.NewRequest:", err)
		return nil, err
	}

	req.Close = true
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth.AccessToken))
	req.Header.Set("Content-Type", "application/json")

	client := createHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error client.Do(req):", err)
		return nil, err
	}

	if resp.Body == nil {
		return nil, errors.New("Config body is nil")
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error ioutil.ReadAll(resp.Body):", err)
		return nil, err
	}

	cfg := config{}
	err = json.Unmarshal(body, &cfg)
	if err != nil {
		log.Printf("Error unmarshalling body: %v\nbody: %s", err, string(body))
		return nil, err
	}

	return &cfg, nil
}

func createHTTPClient() *http.Client {
	transport := &http.Transport{}

	return &http.Client{
		Transport: transport,
	}
}
