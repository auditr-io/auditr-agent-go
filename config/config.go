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
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
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
	BaseURL              string  `json:"base_url"`
	EventsPath           string  `json:"events_path"`
	TargetRoutes         []Route `json:"target"`
	SampleRoutes         []Route `json:"sample"`
	CacheDuration        uint    `json:"cache_duration"`
	Flush                bool    `json:"flush"`
	MaxEventsPerBatch    uint    `json:"max_events_per_batch"`
	MaxConcurrentBatches uint    `json:"max_concurrent_batches"`
	PendingWorkCapacity  uint    `json:"pending_work_capacity"`
	SendInterval         uint    `json:"send_interval"`
	BlockOnSend          bool    `json:"block_on_send"`
	BlockOnResponse      bool    `json:"block_on_response"`
}

// ConfigOption is an option to override defaults
type ConfigOption func(args ...interface{}) error

// Seed configuration
var (
	// ConfigURL is the seed URL to get the rest of the configuration
	ConfigURL string

	// APIKey is the API key to use with API calls
	APIKey string

	GetClient       ClientProvider = DefaultClientProvider
	configClient    ClientProvider = DefaultConfigClientProvider
	cfgClient       *http.Client
	cfgClientOnce   sync.Once
	clientOverriden bool

	authClient     *http.Client
	authClientOnce sync.Once

	cacheTicker *time.Ticker
	cancelFunc  context.CancelFunc
	filec       chan struct{}
)

// Acquired configuration
var (
	BaseURL       string
	EventsURL     string
	TargetRoutes  []Route
	SampleRoutes  []Route
	Flush         bool
	cacheDuration time.Duration = 5 * 60 * time.Second

	MaxEventsPerBatch    uint
	MaxConcurrentBatches uint
	PendingWorkCapacity  uint
	SendInterval         time.Duration
	BlockOnSend          bool
	BlockOnResponse      bool
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
	viper.BindEnv("auditr_api_key")

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
	APIKey = viper.GetString("auditr_api_key")

	ensureSeedConfig()
	ctx := context.Background()
	ctx, cancelFunc = context.WithCancel(ctx)

	if _, err := watchFile(ctx, "/tmp"); err != nil {
		log.Printf("Error watching file: %+v", err)
	}

	if clientOverriden {
		configure(ctx)
	} else {
		filec = make(chan struct{})
		configureFromFile(ctx)
		<-filec
	}
	return nil
}

// DefaultClientProvider returns the default HTTP client with authorization parameters
func DefaultClientProvider(ctx context.Context) *http.Client {
	client, err := NewHTTPClient(EventsURL)
	if err != nil {
		log.Fatalln("Failed to create events HTTP client")
	}

	return client

	// authClientOnce.Do(func() {
	// 	httpClient, err := NewHTTPClientWithSettings(HTTPClientSettings{
	// 		Connect:          2 * time.Second,
	// 		ExpectContinue:   1 * time.Second,
	// 		IdleConn:         90 * time.Second,
	// 		ConnKeepAlive:    30 * time.Second,
	// 		MaxAllIdleConns:  100,
	// 		MaxHostIdleConns: 10,
	// 		ResponseHeader:   2 * time.Second,
	// 		TLSHandshake:     2 * time.Second,
	// 	})
	// 	if err != nil {
	// 		log.Fatalf("Failed to create HTTP client")
	// 	}

	// 	authClient = httpClient
	// })
	// return authClient
}

// DefaultConfigClientProvider returns the default HTTP client with authorization parameters
func DefaultConfigClientProvider(ctx context.Context) *http.Client {
	client, err := NewHTTPClient(ConfigURL)
	if err != nil {
		log.Fatalln("Failed to create config HTTP client")
	}

	return client

	// cfgClientOnce.Do(func() {
	// 	httpClient, err := NewHTTPClientWithSettings(HTTPClientSettings{
	// 		Connect:          2 * time.Second,
	// 		ExpectContinue:   1 * time.Second,
	// 		IdleConn:         90 * time.Second,
	// 		ConnKeepAlive:    30 * time.Second,
	// 		MaxAllIdleConns:  100,
	// 		MaxHostIdleConns: 10,
	// 		ResponseHeader:   2 * time.Second,
	// 		TLSHandshake:     2 * time.Second,
	// 	})
	// 	if err != nil {
	// 		log.Fatalf("Failed to create HTTP client")
	// 	}

	// 	cfgClient = httpClient
	// })
	// return cfgClient
}

// ensureSeedConfig ensures seed config is provided
func ensureSeedConfig() {
	if ConfigURL == "" {
		// Should never happen due to default
		log.Fatal("ConfigURL must be set")
	}

	if APIKey == "" {
		log.Fatal("AUDITR_API_KEY must be set")
	}
}

func configure(ctx context.Context) error {
	const maxAttempts = uint64(2)
	bc := backoff.WithContext(backoff.NewExponentialBackOff(), ctx)
	bo := backoff.WithMaxRetries(bc, maxAttempts)
	op := func() error {
		body, err := getConfig(ctx)
		if err != nil {
			return err
		}

		err = setConfig(body)
		if err != nil {
			return err
		}

		refresh(ctx)
		return nil
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

func watchFile(ctx context.Context, path string) (<-chan struct{}, error) {
	done := make(chan struct{})
	t1 := time.Now()
	log.Println("watcher waiting for config file")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return done, err
	}

	go func() {
		defer watcher.Close()

		for {
			select {
			case <-ctx.Done():
				log.Println("watcher done")
				close(done)
				return
			case event, ok := <-watcher.Events:
				if !ok {
					log.Println("watcher event not ok")
					continue
				}
				if event.Op&fsnotify.Create == fsnotify.Create ||
					event.Op&fsnotify.Write == fsnotify.Write {
					log.Printf("watcher event: name %s, op: %s", event.Name, event.Op)
					if event.Name == "/tmp/config" {
						log.Printf("watcher config file found [%dms]", time.Since(t1).Milliseconds())
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					log.Println("watcher error not ok")
					continue
				}
				log.Printf("error: %+v", err)
			}
		}
	}()

	if err := watcher.Add(path); err != nil {
		return done, err
	}

	return done, nil
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
					if BaseURL == "" {
						log.Printf("config file found [%dms]", time.Since(t1).Milliseconds())
						body, err := getConfigFromFile()
						if err != nil {
							log.Println("Error reading config file", err)
						} else {
							if len(body) == 0 {
								log.Println("Config body is still empty. Wait 10ms")
							} else {
								setConfig(body)
							}
						}
					}
				}

				if BaseURL == "" {
					tkr.Reset(10 * time.Millisecond)
					continue
				}

				log.Println("Configured")
				filec <- struct{}{}
				tkr.Reset(cacheDuration)
			}
		}
	}()

	return nil
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
		cacheDuration = time.Duration(c.CacheDuration * uint(time.Second))
	}

	Flush = c.Flush

	MaxEventsPerBatch = c.MaxEventsPerBatch
	MaxConcurrentBatches = c.MaxConcurrentBatches
	PendingWorkCapacity = c.PendingWorkCapacity
	SendInterval = time.Duration(c.SendInterval * uint(time.Millisecond))
	BlockOnSend = c.BlockOnSend
	BlockOnResponse = c.BlockOnResponse

	return nil
}

// getConfig acquires configuration from the seed URL
// func getConfig(ctx context.Context) error {
// 	body, err := getConfigFromURL(ctx)

// 	var c *config
// 	err = json.Unmarshal(body, &c)
// 	if err != nil {
// 		log.Printf("Error unmarshalling body - Error: %s, Body: %s", err, string(body))
// 		return err
// 	}

// 	BaseURL = c.BaseURL

// 	url, err := url.Parse(c.BaseURL)
// 	if err != nil {
// 		log.Printf("Error parsing BaseURL: %s", c.BaseURL)
// 		return err
// 	}
// 	url.Path = path.Join(url.Path, c.EventsPath)
// 	EventsURL = url.String()

// 	TargetRoutes = c.TargetRoutes
// 	SampleRoutes = c.SampleRoutes
// 	if c.CacheDuration > 0 {
// 		cacheDuration = time.Duration(c.CacheDuration * int64(time.Second))
// 	}

// 	refresh(ctx)

// 	return nil
// }

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

func getConfig(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ConfigURL, nil)
	if err != nil {
		log.Printf("Error creating request: %s", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

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
