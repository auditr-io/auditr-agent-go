package config

import (
	"context"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/auditr-io/httpclient"
)

type FetcherOptions struct {
	ConfigURL     string
	HTTPTransport http.RoundTripper
}

type Fetcher struct {
	httpTransport http.RoundTripper
	httpClient    *http.Client
	configURL     string
	lastRefreshed time.Time
}

func NewFetcher(opts FetcherOptions) (*Fetcher, error) {
	ensureSeedConfig()

	f := &Fetcher{
		httpTransport: opts.HTTPTransport,
		configURL:     ConfigURL,
		lastRefreshed: time.Now().Add(-CacheDuration),
	}

	if opts.ConfigURL != "" {
		f.configURL = opts.ConfigURL
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

func (f *Fetcher) Refresh(ctx context.Context) error {
	if time.Since(f.lastRefreshed) < CacheDuration {
		return nil
	}

	cfg, err := f.GetConfig()
	if err != nil {
		return err
	}

	if err := os.WriteFile(ConfigPath, cfg, 0644); err != nil {
		return err
	}

	return nil
}

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
