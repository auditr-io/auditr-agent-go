package config

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestRefresh_SetsConfiguration(t *testing.T) {
	configBytes := []byte(`{
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
	var expectedConfig *Configuration
	json.Unmarshal(configBytes, &expectedConfig)

	c, err := NewConfigurer(
		WithConfigProvider(
			func() ([]byte, error) {
				return configBytes, nil
			},
		),
	)
	assert.NoError(t, err)

	ctx := context.Background()
	err = c.Refresh(ctx)
	assert.NoError(t, err)

	c.OnRefresh(func() {})

	assert.Equal(t, expectedConfig.BaseURL, BaseURL)
	expectedEventsURL, err := url.Parse(expectedConfig.BaseURL)
	assert.NoError(t, err)
	expectedEventsURL.Path = path.Join(expectedEventsURL.Path, expectedConfig.EventsPath)
	assert.Equal(t, expectedEventsURL.String(), EventsURL)
	assert.Equal(t, expectedConfig.TargetRoutes, TargetRoutes)
	assert.Equal(t, expectedConfig.SampleRoutes, SampleRoutes)
	assert.Equal(t, expectedConfig.CacheDuration, CacheDuration)
	assert.Equal(t, expectedConfig.Flush, Flush)
	assert.Equal(t, expectedConfig.MaxEventsPerBatch, MaxEventsPerBatch)
	assert.Equal(t, expectedConfig.MaxConcurrentBatches, MaxConcurrentBatches)
	assert.Equal(t, expectedConfig.PendingWorkCapacity, PendingWorkCapacity)
	assert.Equal(t, expectedConfig.SendInterval, SendInterval)
	assert.Equal(t, expectedConfig.BlockOnSend, BlockOnSend)
	assert.Equal(t, expectedConfig.BlockOnResponse, BlockOnResponse)
}

func TestRefresh_HasFreshConfig(t *testing.T) {
	configs := []struct {
		bytes  []byte
		config *Configuration
	}{
		{
			bytes: []byte(`{
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
			}`),
		},
		{
			bytes: []byte(`{
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
				"cache_duration": 5,
				"max_events_per_batch": 10,
				"max_concurrent_batches": 10,
				"pending_work_capacity": 20,
				"send_interval": 20,
				"block_on_send": false,
				"block_on_response": true
			}`),
		},
	}

	for i := range configs {
		json.Unmarshal(configs[i].bytes, &configs[i].config)
	}

	fileEventChan := make(chan fsnotify.Event)

	configProviders := func() func() ([]byte, error) {
		i := 0
		return func() ([]byte, error) {
			bytes := configs[i].bytes
			i = (i + 1) % len(configs)
			return bytes, nil
		}
	}

	configIter := configProviders()
	c, err := NewConfigurer(
		WithConfigProvider(configIter),
		WithFileEventChan(fileEventChan),
	)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < len(configs); i++ {
			cfg := <-c.configuredc
			assert.Equal(t, configs[i].config.CacheDuration, cfg.CacheDuration)

			if i == 0 {
				fileEventChan <- fsnotify.Event{
					Op:   fsnotify.Write,
					Name: ConfigPath,
				}
			}
		}
	}()

	ctx := context.Background()
	err = c.Refresh(ctx)
	assert.NoError(t, err)

	wg.Wait()
}

func TestOnRefresh_ParallelRegistration(t *testing.T) {
	configBytes := []byte(`{
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
	var expectedConfig *Configuration
	json.Unmarshal(configBytes, &expectedConfig)

	c, err := NewConfigurer(
		WithConfigProvider(
			func() ([]byte, error) {
				return configBytes, nil
			},
		),
	)
	assert.NoError(t, err)

	m := mock.Mock{}
	m.On("work1").Return().Once()
	m.On("work2").Return().Once()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()

		c.OnRefresh(func() {
			m.MethodCalled("work1")
			assert.Equal(t, expectedConfig.BaseURL, c.Configuration.BaseURL)
		})
	}()

	go func() {
		defer wg.Done()

		c.OnRefresh(func() {
			m.MethodCalled("work2")
			assert.Equal(t, expectedConfig.BaseURL, c.Configuration.BaseURL)
		})
	}()

	wg.Wait()

	ctx := context.Background()
	err = c.Refresh(ctx)
	assert.NoError(t, err)

	<-c.Configured()

	m.AssertExpectations(t)
}

func TestOnRefresh_RefreshesAsManyTimes(t *testing.T) {
	configBytes := []byte(`{
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
	var expectedConfig *Configuration
	json.Unmarshal(configBytes, &expectedConfig)

	c, err := NewConfigurer(
		WithConfigProvider(
			func() ([]byte, error) {
				return configBytes, nil
			},
		),
	)
	assert.NoError(t, err)

	m := mock.Mock{}
	m.On("work").Return().Twice()

	c.OnRefresh(func() {
		m.MethodCalled("work")
		assert.Equal(t, expectedConfig.BaseURL, c.Configuration.BaseURL)
	})

	ctx := context.Background()
	err = c.Refresh(ctx)
	assert.NoError(t, err)

	<-c.Configured()

	c.lastRefreshed = time.Now().Add(-c.Configuration.CacheDuration)
	err = c.Refresh(ctx)
	assert.NoError(t, err)

	<-c.Configured()

	m.AssertExpectations(t)
}

func TestRefresh_SkipsIfFresh(t *testing.T) {
	mockConfigProvider := mock.Mock{}

	c, err := NewConfigurer(
		WithConfigProvider(
			func() ([]byte, error) {
				mockConfigProvider.MethodCalled("getConfig")
				return nil, errors.New("should not be called")
			},
		),
	)
	assert.NoError(t, err)

	mockConfigProvider.On("getConfig").Return()

	c.lastRefreshed = time.Now()
	ctx := context.Background()
	err = c.Refresh(ctx)
	assert.NoError(t, err)

	mockConfigProvider.AssertNotCalled(t, "getConfig")
}

func TestRefresh_CancelsRunningWatcher(t *testing.T) {
	c, err := NewConfigurer(
		WithConfigProvider(
			func() ([]byte, error) {
				return nil, os.ErrNotExist
			},
		),
	)
	assert.NoError(t, err)

	// first refresh to setup watcher
	err = c.Refresh(context.Background())
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		assert.Equal(t, struct{}{}, <-c.watcherDonec)
	}()

	// second refresh should cancel first watcher
	// and setup another
	err = c.Refresh(context.Background())
	assert.NoError(t, err)

	wg.Wait()
}
