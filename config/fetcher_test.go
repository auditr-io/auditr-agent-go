package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/auditr-io/testmock"
	"github.com/stretchr/testify/assert"
)

func TestGetConfig(t *testing.T) {
	wantCfg := []byte(`{
		"parent_org_id": "parent-org-id",
		"org_id_field": "request.header.x-org-id",
		"base_url": "https://dev-api.auditr.io/v1",
		"events_path": "/events",
		"target": [
			{
				"method": "GET",
				"path": "/person/:id"
			}
		],
		"sample": [],
		"flush": false,
		"cache_duration": 2,
		"max_events_per_batch": 10,
		"max_concurrent_batches": 10,
		"pending_work_capacity": 20,
		"send_interval": 20,
		"block_on_send": false,
		"block_on_response": true
	}`)

	m := &testmock.MockTransport{
		RoundTripFn: func(m *testmock.MockTransport, req *http.Request) (*http.Response, error) {
			assert.Equal(t, APIKey, req.Header.Get("Authorization"))

			return &http.Response{
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBuffer(wantCfg)),
			}, nil
		},
	}

	f, err := NewFetcher(FetcherOptions{
		ConfigURL:     "https://" + t.Name() + ".auditr.io",
		HTTPTransport: m,
	})
	assert.NoError(t, err)

	cfg, err := f.GetConfig()
	assert.NoError(t, err)
	assert.Equal(t, wantCfg, cfg)
}

func TestRefresh(t *testing.T) {
	wantRefreshes := 3
	refreshes := 0
	var cfgCache []byte

	wantCfgs := make([][]byte, wantRefreshes)
	for i := 0; i < wantRefreshes; i++ {
		wantCfgs[i] = []byte(fmt.Sprintf(`{
			"parent_org_id": "parent-org-id",
			"org_id_field": "request.header.x-org-id",
			"base_url": "https://dev-api.auditr.io/v1",
			"events_path": "/events",
			"target": [
				{
					"method": "GET",
					"path": "/person/:id"
				}
			],
			"sample": [],
			"flush": false,
			"cache_duration": 2,
			"max_events_per_batch": %d,
			"max_concurrent_batches": 10,
			"pending_work_capacity": 20,
			"send_interval": 20,
			"block_on_send": false,
			"block_on_response": true
		}`, i))
	}

	m := &testmock.MockTransport{
		RoundTripFn: func(m *testmock.MockTransport, req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBuffer(wantCfgs[refreshes])),
			}, nil
		},
	}

	f, err := NewFetcher(FetcherOptions{
		ConfigURL:     "https://" + t.Name() + ".auditr.io",
		HTTPTransport: m,
		WriteCache: func(cfg []byte) error {
			cfgCache = cfg
			return nil
		},
		Interval: 10 * time.Millisecond,
	})
	assert.NoError(t, err)

	ctx, cancelf := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case r := <-f.Refreshes():
				assert.Equal(t, wantCfgs[refreshes], r)
				assert.Equal(t, wantCfgs[refreshes], cfgCache)

				refreshes++
				if refreshes == wantRefreshes {
					cancelf()
				}
			}
		}
	}()

	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case err := <-f.Errors():
				assert.Fail(t, err.Error())
			}
		}
	}()

	f.Refresh(ctx)

	wg.Wait()
	assert.Equal(t, wantRefreshes, refreshes)
}

func TestRefreshWithWriteFile(t *testing.T) {
	configPath := "/tmp/" + t.Name() + "-auditr-config"
	t.Cleanup(func() {
		os.Remove(configPath)
	})

	wantRefreshes := 3
	refreshes := 0

	wantCfgs := make([][]byte, wantRefreshes)
	for i := 0; i < wantRefreshes; i++ {
		wantCfgs[i] = []byte(fmt.Sprintf(`{
			"parent_org_id": "parent-org-id",
			"org_id_field": "request.header.x-org-id",
			"base_url": "https://dev-api.auditr.io/v1",
			"events_path": "/events",
			"target": [
				{
					"method": "GET",
					"path": "/person/:id"
				}
			],
			"sample": [],
			"flush": false,
			"cache_duration": 2,
			"max_events_per_batch": %d,
			"max_concurrent_batches": 10,
			"pending_work_capacity": 20,
			"send_interval": 20,
			"block_on_send": false,
			"block_on_response": true
		}`, i))
	}

	m := &testmock.MockTransport{
		RoundTripFn: func(m *testmock.MockTransport, req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBuffer(wantCfgs[refreshes])),
			}, nil
		},
	}

	f, err := NewFetcher(FetcherOptions{
		ConfigURL:     "https://" + t.Name() + ".auditr.io",
		ConfigPath:    configPath,
		HTTPTransport: m,
		Interval:      10 * time.Millisecond,
	})
	assert.NoError(t, err)

	ctx, cancelf := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case r := <-f.Refreshes():
				assert.Equal(t, wantCfgs[refreshes], r)

				cfgf, err := os.Open(configPath)
				assert.NoError(t, err)
				cfg, err := ioutil.ReadAll(cfgf)
				assert.NoError(t, err)
				assert.Equal(t, wantCfgs[refreshes], cfg)

				refreshes++
				if refreshes == wantRefreshes {
					cancelf()
				}
			}
		}
	}()

	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case err := <-f.Errors():
				assert.Fail(t, err.Error())
			}
		}
	}()

	f.Refresh(ctx)

	wg.Wait()
	assert.Equal(t, wantRefreshes, refreshes)
}

func TestRefreshWithErrors(t *testing.T) {
	wantRefreshes := 2
	refreshes := 0

	wantCfgs := make([][]byte, wantRefreshes)
	for i := 0; i < wantRefreshes; i++ {
		wantCfgs[i] = []byte(fmt.Sprintf(`{
			"parent_org_id": "parent-org-id",
			"org_id_field": "request.header.x-org-id",
			"base_url": "https://dev-api.auditr.io/v1",
			"events_path": "/events",
			"target": [
				{
					"method": "GET",
					"path": "/person/:id"
				}
			],
			"sample": [],
			"flush": false,
			"cache_duration": 2,
			"max_events_per_batch": %d,
			"max_concurrent_batches": 10,
			"pending_work_capacity": 20,
			"send_interval": 20,
			"block_on_send": false,
			"block_on_response": true
		}`, i))
	}

	wantErrs := []error{
		errors.New("error getting config"),
		errors.New("error writing cache"),
	}

	m := &testmock.MockTransport{
		RoundTripFn: func(m *testmock.MockTransport, req *http.Request) (*http.Response, error) {
			if refreshes == 0 {
				return nil, wantErrs[refreshes]
			}

			return &http.Response{
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBuffer(wantCfgs[refreshes])),
			}, nil
		},
	}

	f, err := NewFetcher(FetcherOptions{
		ConfigURL:     "https://" + t.Name() + ".auditr.io",
		HTTPTransport: m,
		WriteCache: func(cfg []byte) error {
			if refreshes == 1 {
				return wantErrs[refreshes]
			}

			return nil
		},
		Interval: 10 * time.Millisecond,
	})
	assert.NoError(t, err)

	ctx, cancelf := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case <-f.Refreshes():
				assert.Fail(t, "refresh should have failed")
			}
		}
	}()

	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case err := <-f.Errors():
				if refreshes == 0 {
					if err, ok := err.(*url.Error); ok {
						assert.Equal(t, wantErrs[refreshes], err.Err)
					}
				} else if refreshes == 1 {
					assert.Equal(t, wantErrs[refreshes], err)
				}

				refreshes++
				if refreshes == wantRefreshes {
					cancelf()
				}
			}
		}
	}()

	f.Refresh(ctx)

	wg.Wait()
	assert.Equal(t, wantRefreshes, refreshes)
}
