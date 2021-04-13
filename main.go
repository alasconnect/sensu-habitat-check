package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sensu-community/sensu-plugin-sdk/sensu"
	"github.com/sensu/sensu-go/types"
)

// Config represents the check plugin config.
type Config struct {
	sensu.PluginConfig
	SupervisorURL string
	Services      []string
	Timeout       int
}

var (
	plugin = Config{
		PluginConfig: sensu.PluginConfig{
			Name:     "sensu-habitat-check",
			Short:    "Checks habitat supervisor for service health",
			Keyspace: "sensu.io/plugins/sensu-habitat-check/config",
		},
	}

	options = []*sensu.PluginConfigOption{
		{
			Path:      "supervisor-url",
			Env:       "",
			Argument:  "supervisor-url",
			Shorthand: "u",
			Default:   "http://127.0.0.1:9631",
			Usage:     "Supervisor URL",
			Value:     &plugin.SupervisorURL,
		},
		{
			Path:      "service",
			Env:       "",
			Argument:  "service",
			Shorthand: "s",
			Default:   []string{},
			Usage:     "Explicit service to check, in format service_name.service_group",
			Value:     &plugin.Services,
		},
		{
			Path:      "timeout",
			Env:       "",
			Argument:  "timeout",
			Shorthand: "t",
			Default:   15,
			Usage:     "Request timeout in seconds",
			Value:     &plugin.Timeout,
		},
	}
)

func main() {
	check := sensu.NewGoCheck(&plugin.PluginConfig, options, checkArgs, executeCheck, false)
	check.Execute()
}

func checkArgs(event *types.Event) (int, error) {
	if len(plugin.Services) > 0 {
		for _, service := range plugin.Services {
			serviceSplit := strings.SplitN(service, ".", 2)
			if len(serviceSplit) != 2 {
				return sensu.CheckStateWarning, fmt.Errorf("--service %q value malformed should be \"service_name.service_group\"", service)
			}
		}
	}

	_, err := url.Parse(plugin.SupervisorURL)
	if err != nil {
		return sensu.CheckStateWarning, fmt.Errorf("failed to parse supervisor URL %s: %v", plugin.SupervisorURL, err)
	}

	return sensu.CheckStateOK, nil
}

type ServiceResponse []struct {
	ServiceGroup string `json:"service_group"`
}

type HealthResponse struct {
	ServiceGroup string `json:"service_group"`
	Status       string `json:"status"`
}

func executeCheck(event *types.Event) (int, error) {
	client := http.DefaultClient
	client.Transport = http.DefaultTransport
	client.Timeout = time.Duration(plugin.Timeout) * time.Second

	var err error
	var services = plugin.Services

	if len(services) == 0 {
		services, err = getAllServices(client)
		if err != nil {
			return sensu.CheckStateCritical, fmt.Errorf("could not retrieve services: %v", err)
		}
	}

	var sResp []HealthResponse
	sResp, err = checkServices(services, client)
	if err != nil {
		return sensu.CheckStateCritical, fmt.Errorf("could not retrieve service health: %v", err)
	}

	oks := 0
	warnings := 0
	criticals := 0
	unknowns := 0
	found := false

	for _, s := range sResp {
		found = true
		if strings.EqualFold(s.Status, "ok") {
			oks++
			continue
		} else if strings.EqualFold(s.Status, "warning") {
			warnings++
			fmt.Printf("%s WARNING\n", s.ServiceGroup)
		} else if strings.EqualFold(s.Status, "critical") {
			criticals++
			fmt.Printf("%s CRITICAL\n", s.ServiceGroup)
		} else if strings.EqualFold(s.Status, "unknown") {
			unknowns++
			fmt.Printf("%s UNKNOWN\n", s.ServiceGroup)
		}
	}

	if criticals > 0 || unknowns > 0 {
		return sensu.CheckStateCritical, nil
	} else if warnings > 0 {
		return sensu.CheckStateWarning, nil
	}

	if found {
		fmt.Printf("All health checks returning OK for loaded services")
	} else {
		fmt.Printf("No services loaded")
	}

	return sensu.CheckStateOK, nil
}

func getAllServices(client *http.Client) ([]string, error) {
	req, err := http.NewRequest("GET", plugin.SupervisorURL+"/services", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	var resp *http.Response
	resp, err = getResponse(client, req)
	if err != nil {
		return nil, err
	}

	var services ServiceResponse
	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		return nil, fmt.Errorf("failed to decode service response: %v", err)
	}

	var result = make([]string, len(services))
	for i, v := range services {
		result[i] = v.ServiceGroup
	}

	return result, nil
}

func checkServices(services []string, client *http.Client) ([]HealthResponse, error) {
	var result []HealthResponse

	for _, service := range services {
		serviceSplit := strings.SplitN(service, ".", 2)

		req, err := http.NewRequest("GET", plugin.SupervisorURL+"/services/"+serviceSplit[0]+"/"+serviceSplit[1]+"/health", nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Accept", "application/json")

		var resp *http.Response
		resp, err = getResponse(client, req)
		if err != nil {
			return nil, err
		}

		var health HealthResponse
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			return nil, fmt.Errorf("failed to decode health response: %v", err)
		}
		health.ServiceGroup = service

		result = append(result, health)
	}

	return result, nil
}

func getResponse(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}

	return resp, nil
}
