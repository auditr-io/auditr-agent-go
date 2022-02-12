package config

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"os"
	"testing"

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
	t.Cleanup(func() {
		os.Remove(ConfigPath)
	})

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

	err = f.Refresh(context.TODO())
	assert.NoError(t, err)

	cfgf, err := os.Open(ConfigPath)
	assert.NoError(t, err)
	cfg, err := ioutil.ReadAll(cfgf)
	assert.NoError(t, err)
	assert.Equal(t, wantCfg, cfg)
}
