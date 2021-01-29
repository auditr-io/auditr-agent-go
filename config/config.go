package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"

	"github.com/spf13/viper"
	"golang.org/x/oauth2/clientcredentials"
)

// ClientProvider is a function that returns an HTTP client
type ClientProvider func(context.Context) *http.Client

// Route is a route used for targeting or sampling
type Route struct {
	HTTPMethod string `json:"method"`
	Path       string `json:"path"`
}

// config is used to unmarshal acquired configuration
type config struct {
	BaseURL       string  `json:"base_url"`
	EventsPath    string  `json:"events_path"`
	TargetRoutes  []Route `json:"target"`
	SampledRoutes []Route `json:"sampled"`
}

// ConfigOption is an option to override defaults
type ConfigOption func(args ...interface{}) error

// Seed configuration
var (
	// ConfigURL is the seed URL to get the rest of the configuration
	ConfigURL string

	// TokenURL is the URL to authenticate the agent
	TokenURL string

	// Client credentials
	ClientID     string
	ClientSecret string
	GetClient    ClientProvider = DefaultClientProvider

	// auth is an OAuth2 Client Credentials client
	auth *clientcredentials.Config
)

// Acquired configuration
var (
	BaseURL       string
	EventsURL     string
	TargetRoutes  []Route
	SampledRoutes []Route
)

// WithHTTPClient overrides the default HTTP client with given client
func WithHTTPClient(client ClientProvider) ConfigOption {
	return func(args ...interface{}) error {
		GetClient = client
		return nil
	}
}

// Init initializes the configuration before use
func Init(options ...ConfigOption) error {
	for _, opt := range options {
		if err := opt(); err != nil {
			return err
		}
	}

	viper.SetConfigType("env")

	viper.BindEnv("auditr_config_url")
	viper.BindEnv("auditr_token_url")
	viper.BindEnv("auditr_client_id")
	viper.BindEnv("auditr_client_secret")

	viper.SetDefault("auditr_config_url", "https://config.auditr.io")
	viper.SetDefault("auditr_token_url", "https://auth.auditr.io/oauth2/token")

	// If a config file is available, load the env vars in it
	if configFile, ok := os.LookupEnv("CONFIG"); ok {
		viper.SetConfigFile(configFile)

		if err := viper.ReadInConfig(); err != nil {
			log.Printf("Error reading config file: %v\n", err)

		}
	}

	ConfigURL = viper.GetString("auditr_config_url")
	TokenURL = viper.GetString("auditr_token_url")
	ClientID = viper.GetString("auditr_client_id")
	ClientSecret = viper.GetString("auditr_client_secret")

	ensureSeedConfig()

	auth = &clientcredentials.Config{
		ClientID:     ClientID,
		ClientSecret: ClientSecret,
		Scopes:       []string{"/events/write"},
		TokenURL:     TokenURL,
	}

	ctx := context.Background()
	const maxAttempts = 2
	for i := 0; i <= maxAttempts; i++ {
		if err := getConfig(ctx); err != nil {
			log.Println(err)
			if i == maxAttempts {
				log.Fatalf("Failed to get configuration after %d attempts: %s",
					maxAttempts,
					err,
				)
			}
			continue
		}

		break
	}

	return nil
}

// DefaultClientProvider returns the default HTTP client with authorization parameters
func DefaultClientProvider(ctx context.Context) *http.Client {
	return auth.Client(ctx)
}

// ensureSeedConfig ensures seed config is provided
func ensureSeedConfig() {
	if ConfigURL == "" {
		// Should never happen due to default
		log.Fatal("ConfigURL must be set")
	}

	if TokenURL == "" {
		// Should never happen due to default
		log.Fatal("TokenURL must be set")
	}

	if ClientID == "" {
		log.Fatal("ClientID must be set using AUDITR_CLIENT_ID")
	}

	if ClientSecret == "" {
		log.Fatal("ClientSecret must be set using AUDITR_CLIENT_SECRET")
	}
}

// getConfig acquires configuration from the seed URL
func getConfig(ctx context.Context) error {
	req, err := http.NewRequest(http.MethodGet, ConfigURL, nil)
	if err != nil {
		log.Printf("Error creating request: %s", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := GetClient(ctx).Do(req)
	if err != nil {
		log.Printf("Error getting config: %s", err)
		return err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)

	if res.StatusCode < 200 || res.StatusCode > 299 {
		if err != nil {
			log.Printf("Error reading response: %s", err)
			return err
		}

		return fmt.Errorf("Error getting config - Status: %d, Response: %s", res.StatusCode, string(body))
	}

	var c *config
	err = json.Unmarshal(body, &c)
	if err != nil {
		log.Printf("Error unmarshalling body - Error: %s, Body: %s", err, string(body))
		return err
	}

	BaseURL = c.BaseURL

	url, err := url.Parse(c.BaseURL)
	if err != nil {
		log.Printf("Error parsing BaseURL: %s", c.BaseURL)
		return err
	}
	url.Path = path.Join(url.Path, c.EventsPath)
	EventsURL = url.String()

	TargetRoutes = c.TargetRoutes
	SampledRoutes = c.SampledRoutes

	return nil
}
