package config

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
)

var (
	BaseUrl      string
	EventsUrl    string
	TargetRoutes []string
)

func init() {
	cfg, err := getConfig()
	if err != nil {
		log.Fatalln("Error getting config:", err)
	}

	BaseUrl = cfg["base_url"].(string)
	EventsUrl = BaseUrl + "/events"

	// TODO: get from client config
	TargetRoutes = []string{
		"/events",
		"/events/:id",
		"/hello/:name",
	}
}

func getConfig() (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", "https://config.auditr.io", nil)
	if err != nil {
		log.Println("Error http.NewRequest:", err)
		return nil, err
	}

	req.Close = true
	req.Header.Set("Authorization", "Bearer token")
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

	cfg := map[string]interface{}{}
	err = json.Unmarshal(body, &cfg)
	if err != nil {
		log.Println("Error unmarshalling body:", err)
		return nil, err
	}

	return cfg, nil
}

func createHTTPClient() *http.Client {
	transport := &http.Transport{}

	return &http.Client{
		Transport: transport,
	}
}
