package config

import (
	"context"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/auditr-io/httpclient"
	"github.com/tidwall/gjson"
)

const (
	MinInterval time.Duration = 60 * time.Second
)

// FetcherOptions allow override of defaults
type FetcherOptions struct {
	ConfigURL     string
	Interval      time.Duration
	HTTPTransport http.RoundTripper
	WriteCache    func([]byte) error
}

// Fetcher periodically fetches config and caches the config locally
type Fetcher struct {
	configURL         string
	interval          time.Duration
	intervalOverriden bool
	httpTransport     http.RoundTripper
	writeCache        func([]byte) error

	httpClient *http.Client
	refreshesc chan []byte
	errc       chan error
	ticker     *time.Ticker
}

// NewFetcher creates a new fetcher with given options
func NewFetcher(opts FetcherOptions) (*Fetcher, error) {
	ensureSeedConfig()

	f := &Fetcher{
		httpTransport:     opts.HTTPTransport,
		configURL:         ConfigURL,
		intervalOverriden: false,
		writeCache:        WriteFile,
		refreshesc:        make(chan []byte, 1),
		errc:              make(chan error, 1),
	}

	f.setInterval(MinInterval)
	if opts.Interval > 0 {
		// set as is for overrides
		f.interval = opts.Interval
		f.intervalOverriden = true
	}

	if opts.ConfigURL != "" {
		f.configURL = opts.ConfigURL
	}

	if opts.WriteCache != nil {
		f.writeCache = opts.WriteCache
	}

	c, err := httpclient.NewClient(
		f.configURL,
		f.httpTransport,
		http.Header{
			"Authorization": []string{APIKey},
		},
	)
	if err != nil {
		return nil, err
	}
	f.httpClient = c

	return f, nil
}

func (f *Fetcher) setInterval(interval time.Duration) {
	if f.intervalOverriden {
		return
	}

	// set a random, slightly earlier interval
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	f.interval = interval - time.Duration(r.Intn(10))*time.Second
	if f.interval <= 0 {
		// shouldn't happen. just in case
		f.interval = MinInterval + f.interval
	}

	if f.ticker != nil {
		f.ticker.Reset(f.interval)
	}
}

// Refresh sets up the interval to fetch a fresh config
func (f *Fetcher) Refresh(ctx context.Context) {
	// don't wait for the first interval
	f.fetchAndCache()

	f.ticker = time.NewTicker(f.interval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				f.ticker.Stop()
				return

			case <-f.ticker.C:
				f.fetchAndCache()
			}
		}
	}()
}

// fetchAndCache fetches and caches config
func (f *Fetcher) fetchAndCache() {
	cfg, err := f.GetConfig()
	if err != nil {
		f.errc <- err
		return
	}

	if err := f.writeCache(cfg); err != nil {
		f.errc <- err
		return
	}

	f.refreshesc <- cfg

	cd := gjson.Get(string(cfg), "cache_duration")
	f.setInterval(time.Duration(cd.Int() * int64(time.Second)))
}

// GetConfig gets a fresh config
func (f *Fetcher) GetConfig() ([]byte, error) {
	res, err := f.httpClient.Get(f.configURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

// Refreshes returns the stream of refreshed configs
// Config may be nil if refresh failed
func (f *Fetcher) Refreshes() <-chan []byte {
	return f.refreshesc
}

// Errors returns the stream of errors
func (f *Fetcher) Errors() <-chan error {
	return f.errc
}

// WriteFile caches the config at ConfigPath
func WriteFile(cfg []byte) error {
	return os.WriteFile(ConfigPath, cfg, 0644)
}
