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
	Status string `json:"status"`
}

type Health struct {
	ServiceGroup string
	Status       int
	Error        error
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

	health := checkServices(services, client)

	oks := 0
	warnings := 0
	criticals := 0
	unknowns := 0
	found := false

	for _, h := range health {
		found = true
		switch h.Status {
		case sensu.CheckStateOK:
			oks++
		case sensu.CheckStateWarning:
			warnings++
			fmt.Printf("%s WARNING\n", h.ServiceGroup)
		case sensu.CheckStateCritical:
			criticals++
			fmt.Printf("%s CRITICAL\n", h.ServiceGroup)
		case sensu.CheckStateUnknown:
			unknowns++
			fmt.Printf("%s UNKNOWN\n", h.ServiceGroup)
		}

		if h.Error != nil {
			fmt.Printf("Error occured while checking service:\n%v\n", h.Error)
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
	req, err := http.NewRequest("GET", getSupervisorUrl()+"/services", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

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

func checkServices(services []string, client *http.Client) []Health {
	var result []Health

	for _, service := range services {
		health := checkService(service, client)
		result = append(result, health)
	}

	return result
}

func checkService(service string, client *http.Client) Health {
	var result Health
	result.ServiceGroup = service
	result.Status = sensu.CheckStateUnknown

	serviceSplit := strings.SplitN(service, ".", 2)

	req, err := http.NewRequest("GET", getSupervisorUrl()+"/services/"+serviceSplit[0]+"/"+serviceSplit[1]+"/health", nil)
	if err != nil {
		result.Error = err
		return result
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		result.Error = err
		return result
	}

	defer resp.Body.Close()

	// a service that isn't loaded or has been stopped returns a 404
	if resp.StatusCode == 200 {
		var hResp HealthResponse
		if err := json.NewDecoder(resp.Body).Decode(&hResp); err != nil {
			result.Error = fmt.Errorf("failed to decode health response: %v", err)
		} else {
			if strings.EqualFold(hResp.Status, "ok") {
				result.Status = sensu.CheckStateOK
			} else if strings.EqualFold(hResp.Status, "warning") {
				result.Status = sensu.CheckStateWarning
			} else if strings.EqualFold(hResp.Status, "critical") {
				result.Status = sensu.CheckStateCritical
			} else if strings.EqualFold(hResp.Status, "unknown") {
				result.Status = sensu.CheckStateUnknown
			}
		}
	}

	return result
}

func getSupervisorUrl() string {
	// a trailing slash will cause errors
	return strings.TrimSuffix(plugin.SupervisorURL, "/")
}
