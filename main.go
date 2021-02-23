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
	// Services      []string
	Timeout int
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
		// {
		// 	Path:      "service",
		// 	Env:       "",
		// 	Argument:  "service",
		// 	Shorthand: "s",
		// 	Default:   []string{},
		// 	Usage:     "Explicit service to check, in format service_name.service_group",
		// 	Value:     &plugin.Services,
		// },
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
	// if len(plugin.Services) > 0 {
	// 	for _, service := range plugin.Services {
	// 		serviceSplit := strings.SplitN(service, ".", 2)
	// 		if len(serviceSplit) != 2 {
	// 			return sensu.CheckStateWarning, fmt.Errorf("--service %q value malformed should be \"service_name.service_group\"", service)
	// 		}
	// 	}
	// }

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

	_, err := url.Parse(plugin.SupervisorURL)
	if err != nil {
		return sensu.CheckStateWarning, fmt.Errorf("Failed to parse supervisor URL %s: %v", plugin.SupervisorURL, err)
	}

	req, err := http.NewRequest("GET", plugin.SupervisorURL+"/services", nil)
	if err != nil {
		return sensu.CheckStateCritical, err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return sensu.CheckStateCritical, err
	}

	defer resp.Body.Close()
	if err != nil {
		return sensu.CheckStateCritical, err
	}

	var sResp ServiceResponse

	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return sensu.CheckStateCritical, fmt.Errorf("Failed to decode service response: %v", err)
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
