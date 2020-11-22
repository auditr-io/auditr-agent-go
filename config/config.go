package config

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
)

var (
	BaseURL       string
	EventsURL     string
	TargetRoutes  []string
	SampledRoutes []string
)

type config struct {
	BaseURL       string   `json:"base_url"`
	EventsPath    string   `json:"events_path"`
	TargetRoutes  []string `json:"target"`
	SampledRoutes []string `json:"sampled"`
}

func init() {
	cfg, err := getConfig()
	if err != nil {
		log.Fatalln("Error getting config:", err)
	}

	BaseURL = cfg.BaseURL
	EventsURL = BaseURL + cfg.EventsPath

	TargetRoutes = cfg.TargetRoutes
	SampledRoutes = cfg.SampledRoutes
}

func getConfig() (*config, error) {
	req, err := http.NewRequest("GET", "https://config.auditr.io", nil)
	if err != nil {
		log.Println("Error http.NewRequest:", err)
		return nil, err
	}

	req.Close = true
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Auditr-Org-ID", "1kXXAxhc0J0D7RqKjFTmq91TJ5J") // get from env
	req.Header.Set("Content-Type", "application/json")

	client := createHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error client.Do(req):", err)
		return nil, err
	}

	if resp.Body == nil {
		return nil, errors.New("Config body is nil")
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error ioutil.ReadAll(resp.Body):", err)
		return nil, err
	}

	cfg := config{}
	err = json.Unmarshal(body, &cfg)
	if err != nil {
		log.Println("Error unmarshalling body:", err)
		return nil, err
	}

	return &cfg, nil
}

func createHTTPClient() *http.Client {
	transport := &http.Transport{}

	return &http.Client{
		Transport: transport,
	}
}
