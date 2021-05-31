package config

import (
	"context"
	"encoding/json"
	"errors"
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
	ConfigDir  = "/tmp"
	ConfigPath = ConfigDir + "/config"
)

// Seed configuration
var (
	// APIKey is the API key to use with API calls
	APIKey string
)

// Acquired configuration
var (
	ParentOrgID          string
	OrgIDField           string
	BaseURL              string
	EventsURL            string
	TargetRoutes         []Route
	SampleRoutes         []Route
	Flush                bool
	CacheDuration        time.Duration = 60 * time.Second
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

// Configuration is used to unmarshal acquired configuration
type Configuration struct {
	ParentOrgID          string        `json:"parent_org_id"`
	OrgIDField           string        `json:"org_id_field"`
	BaseURL              string        `json:"base_url"`
	EventsPath           string        `json:"events_path"`
	EventsURL            string        `json:"-"`
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

	Configurer      *Configurer `json:"-"`
	GetEventsClient HTTPClientProvider
}

// UnmarshalJSON deserailizes JSON into configuration
func (c *Configuration) UnmarshalJSON(b []byte) error {
	type configurationAlias Configuration
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

	url, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	url.Path = path.Join(url.Path, c.EventsPath)
	c.EventsURL = url.String()

	if cfg.CacheDurationRaw > 0 {
		c.CacheDuration = time.Duration(cfg.CacheDurationRaw * uint(time.Second))
	}

	c.SendInterval = time.Duration(cfg.SendIntervalRaw * uint(time.Millisecond))

	return nil
}

var (
	configurer     *Configurer
	configurerOnce sync.Once
)

// ConfigurerOption is an option to override defaults
type ConfigurerOption func(args ...interface{}) error

// ConfigProvider is a function that returns configuration
type ConfigProvider func() ([]byte, error)

// HTTPClientProvider is a function that returns an HTTP client
type HTTPClientProvider func() *http.Client

// WithConfigProvider overrides the default config provider
func WithConfigProvider(provider ConfigProvider) ConfigurerOption {
	return func(args ...interface{}) error {
		if c, ok := args[0].(*Configurer); ok {
			c.getConfig = provider
			return nil
		}

		return errors.New("failed to override config provider")
	}
}

// WithFileEventChan overrides the default file event channel
func WithFileEventChan(eventc <-chan fsnotify.Event) ConfigurerOption {
	return func(args ...interface{}) error {
		if c, ok := args[0].(*Configurer); ok {
			c.fileEventc = eventc
			return nil
		}

		return errors.New("failed to override file event channel")
	}
}

// WithHTTPClient overrides the default HTTP client with given client
func WithHTTPClient(client HTTPClientProvider) ConfigurerOption {
	return func(args ...interface{}) error {
		if c, ok := args[0].(*Configurer); ok {
			c.getEventsClient = client
			return nil
		}

		return errors.New("failed to override HTTP client provider")
	}
}

// Init initializes the configuration
func Init() error {
	viper.SetConfigType("env")
	viper.BindEnv("auditr_api_key")

	// If a config file is available, load the env vars in it
	if configFile, ok := os.LookupEnv("ENV_PATH"); ok {
		viper.SetConfigFile(configFile)

		if err := viper.ReadInConfig(); err != nil {
			log.Printf("Error reading config file: %v\n", err)
		}
	}

	APIKey = viper.GetString("auditr_api_key")

	ensureSeedConfig()

	var err error
	configurerOnce.Do(func() {
		configurer, err = NewConfigurer()
	})

	err = configurer.Refresh(context.Background())
	if err != nil {
		return err
	}

	<-configurer.Configured()
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
	return configurer.Refresh(ctx)
}

// GetConfig returns the current configuration
func GetConfig() *Configuration {
	return configurer.Configuration
}

// Configured returns a channel for whenever configuration is refreshed
func Configured() <-chan Configuration {
	return configurer.Configured()
}

// Configurer reads and applies configuration from a file
type Configurer struct {
	Configuration *Configuration

	getConfig       ConfigProvider
	getEventsClient HTTPClientProvider

	cancelFunc    context.CancelFunc
	lastRefreshed time.Time

	configuredc chan Configuration

	refreshListeners     []func()
	refreshListenersLock sync.RWMutex

	fileEventc   <-chan fsnotify.Event
	watcherDonec chan struct{}
}

// NewConfigurer creates an instance of configurer
func NewConfigurer(options ...ConfigurerOption) (*Configurer, error) {
	configuration := &Configuration{
		CacheDuration: 60 * time.Second,
	}

	c := &Configurer{
		Configuration:    configuration,
		lastRefreshed:    time.Now().Add(-configuration.CacheDuration),
		configuredc:      make(chan Configuration),
		watcherDonec:     make(chan struct{}),
		refreshListeners: []func(){},
	}

	c.Configuration.Configurer = c

	c.getConfig = c.getConfigFromFile
	c.getEventsClient = DefaultEventsClientProvider

	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

// Refresh refreshes the configuration as the config file
// is updated
func (c *Configurer) Refresh(ctx context.Context) error {
	if time.Since(c.lastRefreshed) < c.Configuration.CacheDuration {
		return nil
	}

	if err := c.configure(); err != nil {
		// ignore error if config file doesn't exist yet
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

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

// OnRefresh executes work upon configuration refresh
// The caller goroutine blocks until the configuration is refreshed
func (c *Configurer) OnRefresh(listener func()) {
	c.refreshListenersLock.Lock()
	c.refreshListeners = append(c.refreshListeners, listener)
	log.Printf("refreshListeners %v", c.refreshListeners)
	c.refreshListenersLock.Unlock()
}

// Configured returns a channel for whenever configuration is refreshed
func (c *Configurer) Configured() <-chan Configuration {
	return c.configuredc
}

// configure reads the config file and applies the configuration
func (c *Configurer) configure() error {
	body, err := c.getConfig()
	if err != nil {
		return err
	}

	if len(body) == 0 {
		return errors.New("Config body is empty")
	}

	if err := c.setConfig(body); err != nil {
		return err
	}

	c.lastRefreshed = time.Now()

	go func() {
		c.configuredc <- *c.Configuration
	}()

	c.refreshListenersLock.RLock()
	for _, listener := range c.refreshListeners {
		log.Printf("listener %p", listener)
		go listener()
	}
	c.refreshListenersLock.RUnlock()

	return nil
}

// getConfigFromFile reads the config file
func (c *Configurer) getConfigFromFile() ([]byte, error) {
	if _, err := os.Stat(ConfigPath); err != nil {
		return nil, err
	}

	cfg, err := os.Open(ConfigPath)
	if err != nil {
		return nil, err
	}
	defer cfg.Close()
	body, err := ioutil.ReadAll(cfg)
	if err != nil {
		return nil, err
	}

	return body, nil
}

// watchConfigFile watches the config file for changes and
// configures the agent
func (c *Configurer) watchConfigFile(ctx context.Context) error {
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
				c.watcherDonec <- struct{}{}
				return
			case event, ok := <-c.fileEventc:
				if !ok {
					continue
				}

				if event.Op&fsnotify.Write != fsnotify.Write {
					continue
				}

				if event.Name != ConfigPath {
					continue
				}

				// todo: emit to metrics chan
				log.Printf("watcher config file found [%dms]", time.Since(c.lastRefreshed).Milliseconds())

				if err := c.configure(); err != nil {
					// todo: emit to debug chan
					log.Printf("watcher error configuring: %+v", err)
					continue
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					continue
				}
				// todo: emit to debug chan
				log.Printf("error: %+v", err)
			}
		}
	}()

	if err := watcher.Add(ConfigDir); err != nil {
		return err
	}

	return nil
}

// setConfig applies the configuration from the file
func (c *Configurer) setConfig(body []byte) error {
	err := json.Unmarshal(body, &c.Configuration)
	if err != nil {
		return err
	}

	c.Configuration.GetEventsClient = c.getEventsClient

	ParentOrgID = c.Configuration.ParentOrgID
	OrgIDField = c.Configuration.OrgIDField
	BaseURL = c.Configuration.BaseURL
	EventsURL = c.Configuration.EventsURL
	TargetRoutes = c.Configuration.TargetRoutes
	SampleRoutes = c.Configuration.SampleRoutes
	CacheDuration = c.Configuration.CacheDuration
	Flush = c.Configuration.Flush
	MaxEventsPerBatch = c.Configuration.MaxEventsPerBatch
	MaxConcurrentBatches = c.Configuration.MaxConcurrentBatches
	PendingWorkCapacity = c.Configuration.PendingWorkCapacity
	SendInterval = c.Configuration.SendInterval
	BlockOnSend = c.Configuration.BlockOnSend
	BlockOnResponse = c.Configuration.BlockOnResponse

	return nil
}
