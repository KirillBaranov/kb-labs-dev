// Package config parses .kb/dev.config.json into strongly-typed Go structs.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// ServiceType distinguishes node from docker services.
type ServiceType string

const (
	// ServiceTypeNode is a process managed by kb-dev directly.
	ServiceTypeNode ServiceType = "node"
	// ServiceTypeDocker is a container managed via docker CLI.
	ServiceTypeDocker ServiceType = "docker"
)

// Config is the top-level dev.config.json structure.
type Config struct {
	Version  string              `json:"version"`
	Name     string              `json:"name"`
	Groups   map[string][]string `json:"groups"`
	Services map[string]Service  `json:"services"`
	Settings Settings            `json:"settings"`
}

// Service defines a single managed service.
type Service struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Group       string            `json:"group,omitempty"`
	Type        ServiceType       `json:"type"`
	Command     string            `json:"command"`
	StopCommand string            `json:"stopCommand,omitempty"`
	Container   string            `json:"container,omitempty"`
	HealthCheck string            `json:"healthCheck,omitempty"`
	Port        int               `json:"port,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	DependsOn   []string          `json:"dependsOn,omitempty"`
	Optional    bool              `json:"optional,omitempty"`
	Note        string            `json:"note,omitempty"`
	Target      string            `json:"target,omitempty"` // future: "local", "docker-compose", "ssh://..."
}

// Settings controls runtime behavior.
type Settings struct {
	LogsDir             string `json:"logsDir"`
	PIDDir              string `json:"pidDir"`
	StartTimeout        int    `json:"startTimeout"`        // milliseconds
	HealthCheckInterval int    `json:"healthCheckInterval"` // milliseconds
}

// Load reads and validates a config from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Settings.LogsDir == "" {
		c.Settings.LogsDir = ".kb/logs/tmp"
	}
	if c.Settings.PIDDir == "" {
		c.Settings.PIDDir = ".kb/tmp"
	}
	if c.Settings.StartTimeout == 0 {
		c.Settings.StartTimeout = 30000
	}
	if c.Settings.HealthCheckInterval == 0 {
		c.Settings.HealthCheckInterval = 1000
	}

	for id, svc := range c.Services {
		if svc.Type == "" {
			svc.Type = ServiceTypeNode
		}
		if svc.Target == "" {
			svc.Target = "local"
		}
		c.Services[id] = svc
	}
}

func (c *Config) validate() error {
	// Check all dependsOn references exist.
	for id, svc := range c.Services {
		for _, dep := range svc.DependsOn {
			if _, ok := c.Services[dep]; !ok {
				return fmt.Errorf("service %q depends on unknown service %q", id, dep)
			}
		}
	}

	// Detect cycles.
	if err := c.detectCycles(); err != nil {
		return err
	}

	// Check port uniqueness.
	ports := make(map[int]string)
	for id, svc := range c.Services {
		if svc.Port == 0 {
			continue
		}
		if other, ok := ports[svc.Port]; ok {
			return fmt.Errorf("port %d used by both %q and %q", svc.Port, other, id)
		}
		ports[svc.Port] = id
	}

	return nil
}

func (c *Config) detectCycles() error {
	const (
		white = 0 // unvisited
		gray  = 1 // in progress
		black = 2 // done
	)

	colors := make(map[string]int)
	var path []string

	var visit func(string) error
	visit = func(id string) error {
		colors[id] = gray
		path = append(path, id)

		svc := c.Services[id]
		for _, dep := range svc.DependsOn {
			switch colors[dep] {
			case gray:
				// Found a cycle.
				cycle := make([]string, len(path)+1)
				copy(cycle, path)
				cycle[len(path)] = dep
				return fmt.Errorf("dependency cycle: %v", cycle)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}

		path = path[:len(path)-1]
		colors[id] = black
		return nil
	}

	for id := range c.Services {
		if colors[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResolveTarget converts a target string into a list of service IDs.
// Empty string = all services. Group name = services in that group. Service name = [name].
func (c *Config) ResolveTarget(target string) ([]string, error) {
	if target == "" {
		return c.allServiceIDs(), nil
	}

	// Check if it's a group.
	if services, ok := c.Groups[target]; ok {
		return services, nil
	}

	// Check if it's a service.
	if _, ok := c.Services[target]; ok {
		return []string{target}, nil
	}

	return nil, fmt.Errorf("unknown service or group: %q", target)
}

// TopoSort returns services in topological order grouped into parallel layers.
// Services within the same layer have no mutual dependencies and can start concurrently.
func (c *Config) TopoSort() ([][]string, error) {
	// Build adjacency: service → its dependencies.
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dep → list of services that depend on it

	for id := range c.Services {
		inDegree[id] = 0
	}
	for id, svc := range c.Services {
		inDegree[id] = len(svc.DependsOn)
		for _, dep := range svc.DependsOn {
			dependents[dep] = append(dependents[dep], id)
		}
	}

	// Kahn's algorithm with layer tracking.
	var layers [][]string
	var queue []string

	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue) // deterministic order

	for len(queue) > 0 {
		layer := make([]string, len(queue))
		copy(layer, queue)
		sort.Strings(layer)
		layers = append(layers, layer)

		var next []string
		for _, id := range queue {
			for _, dep := range dependents[id] {
				inDegree[dep]--
				if inDegree[dep] == 0 {
					next = append(next, dep)
				}
			}
		}
		sort.Strings(next)
		queue = next
	}

	// Check all services were placed.
	total := 0
	for _, layer := range layers {
		total += len(layer)
	}
	if total != len(c.Services) {
		return nil, fmt.Errorf("topological sort failed: cycle detected")
	}

	return layers, nil
}

// Dependents returns all services that transitively depend on the given service.
func (c *Config) Dependents(target string) []string {
	var result []string
	visited := make(map[string]bool)

	var walk func(string)
	walk = func(t string) {
		for id, svc := range c.Services {
			if visited[id] {
				continue
			}
			for _, dep := range svc.DependsOn {
				if dep == t {
					visited[id] = true
					result = append(result, id)
					walk(id)
					break
				}
			}
		}
	}

	walk(target)
	sort.Strings(result)
	return result
}

// GroupOrder returns group names in the order they appear in the config.
func (c *Config) GroupOrder() []string {
	// Since map iteration is non-deterministic, derive order from services.
	seen := make(map[string]bool)
	var order []string
	for _, services := range c.Groups {
		_ = services // just need to iterate keys
	}
	// Hardcoded conventional order (matches dev.sh).
	conventional := []string{"infra", "backend", "execution", "local", "ui", "ui-web"}
	for _, g := range conventional {
		if _, ok := c.Groups[g]; ok {
			seen[g] = true
			order = append(order, g)
		}
	}
	// Append any remaining groups.
	for g := range c.Groups {
		if !seen[g] {
			order = append(order, g)
		}
	}
	return order
}

func (c *Config) allServiceIDs() []string {
	ids := make([]string, 0, len(c.Services))
	for id := range c.Services {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
