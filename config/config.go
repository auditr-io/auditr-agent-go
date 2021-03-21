package config

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/cenkalti/backoff/v4"
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
	SampleRoutes  []Route `json:"sample"`
	CacheDuration int64   `json:"cache_duration"`
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
	ClientID        string
	ClientSecret    string
	GetClient       ClientProvider = DefaultClientProvider
	configClient    ClientProvider = DefaultConfigClientProvider
	clientOverriden bool

	// auth is an OAuth2 Client Credentials client
	auth *clientcredentials.Config
)

// Acquired configuration
var (
	BaseURL       string
	EventsURL     string
	TargetRoutes  []Route
	SampleRoutes  []Route
	cacheDuration time.Duration = 5 * 60 * time.Second
	cacheTicker   *time.Ticker
	cancelFunc    context.CancelFunc
	filec         chan struct{}
)

// WithHTTPClient overrides the default HTTP client with given client
func WithHTTPClient(client ClientProvider) ConfigOption {
	return func(args ...interface{}) error {
		GetClient = client
		configClient = client
		clientOverriden = true
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
	ctx := context.Background()
	ctx, cancelFunc = context.WithCancel(ctx)
	// filec = make(chan struct{})
	// configureFromFile(ctx)

	auth = &clientcredentials.Config{
		ClientID:     ClientID,
		ClientSecret: ClientSecret,
		Scopes:       []string{"/events/write"},
		TokenURL:     TokenURL,
	}

	// var wg sync.WaitGroup
	// wg.Add(1)
	// go func() {
	// 	defer wg.Done()

	// 	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://auditr.io", nil)
	// 	res, err := GetClient(ctx).Do(req)
	// 	if err == nil {
	// 		defer res.Body.Close()
	// 	}
	// }()

	if clientOverriden {
		configure(ctx)
	} else {
		filec = make(chan struct{})
		configureFromFile(ctx)
		<-filec
	}
	// <-filec
	// err := configure(ctx)

	// return err
	// wg.Wait()
	return nil
}

// DefaultClientProvider returns the default HTTP client with authorization parameters
func DefaultClientProvider(ctx context.Context) *http.Client {
	return auth.Client(ctx)
}

// DefaultConfigClientProvider returns the default HTTP client with authorization parameters
func DefaultConfigClientProvider(ctx context.Context) *http.Client {
	return http.DefaultClient
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

func configure(ctx context.Context) error {
	const maxAttempts = uint64(2)
	bc := backoff.WithContext(backoff.NewExponentialBackOff(), ctx)
	bo := backoff.WithMaxRetries(bc, maxAttempts)
	op := func() error {
		return getConfig(ctx)
	}

	t := time.Now()
	log.Println("configuring")
	if err := backoff.Retry(op, bo); err != nil {
		log.Fatalf("Failed to get configuration after %d attempts: %s",
			maxAttempts,
			err,
		)
	}
	log.Printf("configured [%dms]", time.Since(t).Milliseconds())

	return nil
}

func configureFromFile(ctx context.Context) error {
	t1 := time.Now()
	log.Println("waiting for config file")
	tkr := time.NewTicker(100 * time.Millisecond)
	go func() {
		for {
			select {
			case <-tkr.C:
				if _, err := os.Stat("/tmp/config"); err == nil {
					log.Printf("config file found [%dms]", time.Since(t1).Milliseconds())
					body, err := getConfigFromFile()
					if err != nil {
						log.Println("Error reading config file", err)
						return
					}
					if len(body) == 0 {
						log.Println("Body is still empty. Wait 10ms")
						// cancelFunc()
						tkr.Reset(10 * time.Millisecond)
						return
					}
					setConfig(body)
					filec <- struct{}{}
					tkr.Stop()
					return
				}

				tkr.Reset(10 * time.Millisecond)
				// filec <- struct{}{}
			}
		}
	}()

	return nil

	// watcher, err := fsnotify.NewWatcher()
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// defer watcher.Close()

	// go func() {
	// 	for {
	// 		select {
	// 		case event, ok := <-watcher.Events:
	// 			if !ok {
	// 				return
	// 			}
	// 			if event.Op&fsnotify.Write == fsnotify.Write ||
	// 				event.Op&fsnotify.Create == fsnotify.Create {
	// 				body, _ := getConfigFromFile()
	// 				setConfig(body)
	// 			}
	// 		}
	// 	}
	// }()

	// err = watcher.Add("/tmp")
	// if err != nil {
	// 	log.Println("Error watching tmp", err)
	// }

	// return nil
}

func setConfig(body []byte) error {
	var c *config
	err := json.Unmarshal(body, &c)
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
	SampleRoutes = c.SampleRoutes
	if c.CacheDuration > 0 {
		cacheDuration = time.Duration(c.CacheDuration * int64(time.Second))
	}

	return nil
}

// getConfig acquires configuration from the seed URL
func getConfig(ctx context.Context) error {
	body, err := getConfigFromURL(ctx)

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
	SampleRoutes = c.SampleRoutes
	if c.CacheDuration > 0 {
		cacheDuration = time.Duration(c.CacheDuration * int64(time.Second))
	}

	refresh(ctx)

	return nil
}

func getConfigFromFile() ([]byte, error) {
	t1 := time.Now()
	log.Println("get config file")
	cfg, err := os.Open("/tmp/config")
	if err != nil {
		return nil, err
	}
	defer cfg.Close()
	body, err := ioutil.ReadAll(cfg)
	if err != nil {
		return nil, err
	}

	log.Printf("got config file [%dms]", time.Since(t1).Milliseconds())
	return body, nil
}

func getConfigFromURL(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ConfigURL, nil)
	if err != nil {
		log.Printf("Error creating request: %s", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	sec := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", ClientID, ClientSecret)))
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", sec))

	t1 := time.Now()
	log.Println("get config url")
	res, err := configClient(ctx).Do(req)
	log.Printf("got config url [%dms]", time.Since(t1).Milliseconds())
	if err != nil {
		log.Printf("Error getting config: %s", err)
		return nil, err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)

	if res.StatusCode < 200 || res.StatusCode > 299 {
		if err != nil {
			log.Printf("Error reading response: %s", err)
			return nil, err
		}

		return nil, fmt.Errorf("Error getting config - Status: %d, Response: %s", res.StatusCode, string(body))
	}

	return body, nil
}

func refresh(ctx context.Context) {
	if cacheTicker != nil {
		cacheTicker.Reset(cacheDuration)
	} else {
		cacheTicker = time.NewTicker(cacheDuration)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cacheTicker.C:
				cacheTicker.Stop()
				configure(ctx)
				cacheTicker.Reset(cacheDuration)
			}
		}
	}()
}
