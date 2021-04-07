package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

const (
	configDir  = "/tmp"
	configPath = configDir + "/config"
)

// Seed configuration
var (
	// APIKey is the API key to use with API calls
	APIKey string
)

// Acquired configuration
var (
	BaseURL       string
	EventsURL     string
	TargetRoutes  []Route
	SampleRoutes  []Route
	Flush         bool
	CacheDuration time.Duration = 60 * time.Second

	MaxEventsPerBatch    uint
	MaxConcurrentBatches uint
	PendingWorkCapacity  uint
	SendInterval         time.Duration
	BlockOnSend          bool
	BlockOnResponse      bool
)

// Route is a route used for targeting or sampling
type Route struct {
	HTTPMethod string `json:"method"`
	Path       string `json:"path"`
}

// configuration is used to unmarshal acquired configuration
type configuration struct {
	BaseURL              string        `json:"base_url"`
	EventsPath           string        `json:"events_path"`
	TargetRoutes         []Route       `json:"target"`
	SampleRoutes         []Route       `json:"sample"`
	CacheDuration        time.Duration `json:"-"`
	Flush                bool          `json:"flush"`
	MaxEventsPerBatch    uint          `json:"max_events_per_batch"`
	MaxConcurrentBatches uint          `json:"max_concurrent_batches"`
	PendingWorkCapacity  uint          `json:"pending_work_capacity"`
	SendInterval         time.Duration `json:"-"`
	BlockOnSend          bool          `json:"block_on_send"`
	BlockOnResponse      bool          `json:"block_on_response"`
}

// UnmarshalJSON deserailizes JSON into configuration
func (c *configuration) UnmarshalJSON(b []byte) error {
	type configurationAlias configuration
	cfg := &struct {
		CacheDurationRaw uint `json:"cache_duration"`
		SendIntervalRaw  uint `json:"send_interval"`
		*configurationAlias
	}{
		configurationAlias: (*configurationAlias)(c),
	}

	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}

	if cfg.CacheDurationRaw > 0 {
		c.CacheDuration = time.Duration(cfg.CacheDurationRaw * uint(time.Second))
	}

	c.SendInterval = time.Duration(cfg.SendIntervalRaw * uint(time.Millisecond))

	return nil
}

var (
	cfgr     *configurer
	cfgrOnce sync.Once

	GetEventsClient HTTPClientProvider = DefaultEventsClientProvider
)

// ConfigOption is an option to override defaults
type ConfigOption func(args ...interface{}) error

// configProvider is a function that returns configuration
type configProvider func() ([]byte, error)

// HTTPClientProvider is a function that returns an HTTP client
type HTTPClientProvider func() *http.Client

// WithConfigProvider overrides the default config provider
func WithConfigProvider(provider configProvider) ConfigOption {
	return func(args ...interface{}) error {
		if c, ok := args[0].(*configurer); ok {
			c.getConfig = provider
			return nil
		}

		return errors.New("failed to override config provider")
	}
}

// WithFileEventChan overrides the default file event channel
func WithFileEventChan(eventc <-chan fsnotify.Event) ConfigOption {
	return func(args ...interface{}) error {
		if c, ok := args[0].(*configurer); ok {
			c.fileEventc = eventc
			return nil
		}

		return errors.New("failed to override file event channel")
	}
}

// WithHTTPClient overrides the default HTTP client with given client
func WithHTTPClient(client HTTPClientProvider) ConfigOption {
	return func(args ...interface{}) error {
		GetEventsClient = client
		return nil
	}
}

func Init(options ...ConfigOption) error {
	viper.SetConfigType("env")
	viper.BindEnv("auditr_api_key")

	// todo: rename to env file
	// If a config file is available, load the env vars in it
	if configFile, ok := os.LookupEnv("CONFIG"); ok {
		viper.SetConfigFile(configFile)

		if err := viper.ReadInConfig(); err != nil {
			log.Printf("Error reading config file: %v\n", err)
		}
	}

	APIKey = viper.GetString("auditr_api_key")

	ensureSeedConfig()

	var err error
	cfgrOnce.Do(func() {
		cfgr, err = NewConfigurer(options...)
	})

	err = cfgr.Refresh(context.Background())
	if err != nil {
		return err
	}

	<-cfgr.configuredc
	return nil
}

// DefaultEventsClientProvider returns the default HTTP client with authorization parameters
func DefaultEventsClientProvider() *http.Client {
	client, err := NewHTTPClient(EventsURL)
	if err != nil {
		log.Fatalf("Failed to create events HTTP client: %#v", err)
	}

	return client
}

// ensureSeedConfig ensures seed config is provided
func ensureSeedConfig() {
	if APIKey == "" {
		log.Fatal("AUDITR_API_KEY must be set")
	}
}

// Refresh refreshes the configuration as the config file
// is updated
func Refresh(ctx context.Context) error {
	return cfgr.Refresh(ctx)
}

type configurer struct {
	config *configuration

	getConfig       configProvider
	GetEventsClient HTTPClientProvider

	cancelFunc    context.CancelFunc
	lastRefreshed time.Time
	configuredc   chan configuration
	fileEventc    <-chan fsnotify.Event
	watcherDonec  chan struct{}
}

// NewConfigurer creates an instance of configurer that reads and
// applies configuration from a file
func NewConfigurer(options ...ConfigOption) (*configurer, error) {
	config := &configuration{
		CacheDuration: 60 * time.Second,
	}

	c := &configurer{
		config:        config,
		lastRefreshed: time.Now().Add(-config.CacheDuration),
		configuredc:   make(chan configuration),
		watcherDonec:  make(chan struct{}),
	}

	c.getConfig = c.getConfigFromFile

	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

// Refresh refreshes the configuration as the config file
// is updated
func (c *configurer) Refresh(ctx context.Context) error {
	if time.Since(c.lastRefreshed) < c.config.CacheDuration {
		return nil
	}

	// ignore error if config file doesn't exist yet
	c.configure()

	// if watcher is already running, cancel it
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	ctx, c.cancelFunc = context.WithCancel(ctx)
	if err := c.watchConfigFile(ctx); err != nil {
		return err
	}

	return nil
}

// configure reads the config file and applies the configuration
func (c *configurer) configure() error {
	body, err := c.getConfig()
	if err != nil {
		return err
	}

	if len(body) == 0 {
		return fmt.Errorf("Config body is empty")
	}

	if err := c.setConfig(body); err != nil {
		return err
	}

	c.lastRefreshed = time.Now()
	go func() {
		c.configuredc <- *c.config
	}()

	return nil
}

// getConfigFromFile reads the config file
func (c *configurer) getConfigFromFile() ([]byte, error) {
	t1 := time.Now()
	cfg, err := os.Open(configPath)
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

// watchConfigFile watches the config file for changes and
// configures the agent
func (c *configurer) watchConfigFile(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if c.fileEventc == nil {
		c.fileEventc = watcher.Events
	}

	go func() {
		defer watcher.Close()

		for {
			select {
			case <-ctx.Done():
				log.Println("watcher done")
				c.watcherDonec <- struct{}{}
				return
			case event, ok := <-c.fileEventc:
				if !ok {
					continue
				}

				if event.Op&fsnotify.Write != fsnotify.Write {
					continue
				}

				log.Printf("watcher event: name %s, op: %s", event.Name, event.Op)
				if event.Name != configPath {
					continue
				}

				log.Printf("watcher config file found [%dms]", time.Since(c.lastRefreshed).Milliseconds())

				if err := c.configure(); err != nil {
					log.Printf("watcher error configuring: %+v", err)
					continue
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					continue
				}
				log.Printf("error: %+v", err)
			}
		}
	}()

	if err := watcher.Add(configDir); err != nil {
		return err
	}

	return nil
}

// setConfig applies the configuration from the file
func (c *configurer) setConfig(body []byte) error {
	err := json.Unmarshal(body, &c.config)
	if err != nil {
		log.Printf("Error unmarshalling body - Error: %s, Body: %s", err, string(body))
		return err
	}

	BaseURL = c.config.BaseURL

	url, err := url.Parse(c.config.BaseURL)
	if err != nil {
		log.Printf("Error parsing BaseURL: %s", c.config.BaseURL)
		return err
	}
	url.Path = path.Join(url.Path, c.config.EventsPath)
	EventsURL = url.String()

	TargetRoutes = c.config.TargetRoutes
	SampleRoutes = c.config.SampleRoutes
	CacheDuration = c.config.CacheDuration
	Flush = c.config.Flush
	MaxEventsPerBatch = c.config.MaxEventsPerBatch
	MaxConcurrentBatches = c.config.MaxConcurrentBatches
	PendingWorkCapacity = c.config.PendingWorkCapacity
	SendInterval = c.config.SendInterval
	BlockOnSend = c.config.BlockOnSend
	BlockOnResponse = c.config.BlockOnResponse

	return nil
}

// Init initializes the configuration before use
// func Init(options ...ConfigOption) error {
// 	for _, opt := range options {
// 		if err := opt(); err != nil {
// 			return err
// 		}
// 	}

// 	viper.SetConfigType("env")
// 	viper.BindEnv("auditr_api_key")

// 	// todo: rename to env file
// 	// If a config file is available, load the env vars in it
// 	if configFile, ok := os.LookupEnv("CONFIG"); ok {
// 		viper.SetConfigFile(configFile)

// 		if err := viper.ReadInConfig(); err != nil {
// 			log.Printf("Error reading config file: %v\n", err)

// 		}
// 	}

// 	APIKey = viper.GetString("auditr_api_key")

// 	ensureSeedConfig()

// 	if err := Refresh(context.Background()); err != nil {
// 		log.Printf("Error refreshing config: %+v", err)
// 	}

// 	<-filec

// 	return nil
// }
