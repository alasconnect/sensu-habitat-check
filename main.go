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
	HealthCheck  string `json:"health_check"`
	ServiceGroup string `json:"service_group"`
}

func executeCheck(event *types.Event) (int, error) {
	client := http.DefaultClient
	client.Transport = http.DefaultTransport
	client.Timeout = time.Duration(plugin.Timeout) * time.Second

	var sResp ServiceResponse
	var err error

	if len(plugin.Services) > 0 {
		sResp, err = checkServices(plugin.Services, client)
	} else {
		sResp, err = checkAllServices(client)
	}

	if err != nil {
		return sensu.CheckStateCritical, fmt.Errorf("could not retrive service health: %v", err)
	}

	oks := 0
	warnings := 0
	criticals := 0
	unknowns := 0
	found := false

	for _, s := range sResp {
		found = true
		if strings.EqualFold(s.HealthCheck, "ok") {
			oks++
			continue
		} else if strings.EqualFold(s.HealthCheck, "warning") {
			warnings++
			fmt.Printf("%s WARNING\n", s.ServiceGroup)
		} else if strings.EqualFold(s.HealthCheck, "critical") {
			criticals++
			fmt.Printf("%s CRITICAL\n", s.ServiceGroup)
		} else if strings.EqualFold(s.HealthCheck, "unknown") {
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

func checkAllServices(client *http.Client) (ServiceResponse, error) {
	req, err := http.NewRequest("GET", plugin.SupervisorURL+"/services", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	return getServiceResponse(client, req)
}

func checkServices(services []string, client *http.Client) (ServiceResponse, error) {
	var result ServiceResponse

	for _, service := range services {
		serviceSplit := strings.SplitN(service, ".", 2)

		req, err := http.NewRequest("GET", plugin.SupervisorURL+"/services/"+serviceSplit[0]+"/"+serviceSplit[1], nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Accept", "application/json")
		var sResp ServiceResponse

		sResp, err = getServiceResponse(client, req)
		if err != nil {
			return nil, err
		}

		result = append(result, sResp...)
	}

	return result, nil
}

func getServiceResponse(client *http.Client, req *http.Request) (ServiceResponse, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}

	var result ServiceResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode service response: %v", err)
	}

	return result, nil
}
