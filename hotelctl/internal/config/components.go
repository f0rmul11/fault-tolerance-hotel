package config

import "fmt"

// Dependency describes an infrastructure component and its optional local endpoint.
type Dependency struct {
	Name       string
	Deployment string
	Port       PortBinding
	Database   string
}

func Dependencies() []Dependency {
	return []Dependency{
		{
			Name:       "booking-postgres",
			Deployment: "booking-postgres",
			Port: PortBinding{
				Host:      15432,
				Node:      30432,
				LocalOnly: true,
			},
			Database: "booking",
		},
		{
			Name:       "check-in-postgres",
			Deployment: "check-in-postgres",
			Port: PortBinding{
				Host:      15433,
				Node:      30433,
				LocalOnly: true,
			},
			Database: "checkin",
		},
		{
			Name:       "hotels-postgres",
			Deployment: "hotels-postgres",
			Port: PortBinding{
				Host:      15434,
				Node:      30434,
				LocalOnly: true,
			},
			Database: "hotels",
		},
		{
			Name:       "review-postgres",
			Deployment: "review-postgres",
			Port: PortBinding{
				Host:      15435,
				Node:      30435,
				LocalOnly: true,
			},
			Database: "review",
		},
		{
			Name:       "kafka",
			Deployment: "booking-kafka",
		},
		{
			Name:       "config-etcd",
			Deployment: "config-etcd",
			Port: PortBinding{
				Host:      2379,
				Node:      32379,
				LocalOnly: true,
			},
		},
		{
			Name:       "akhq",
			Deployment: "akhq",
			Port: PortBinding{
				Host:      8090,
				Node:      30089,
				LocalOnly: true,
			},
		},
	}
}

func (c Config) Components() []Service {
	result := make([]Service, 0, len(c.Services)+len(Dependencies()))
	for _, dependency := range Dependencies() {
		result = append(result, Service{
			Name:       dependency.Name,
			Deployment: dependency.Deployment,
			Namespace:  "hotelflow",
		})
	}
	return append(result, c.Services...)
}

func (c Config) SelectComponents(names []string) ([]Service, error) {
	available := c.Components()
	var result []Service
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			return nil, fmt.Errorf("компонент %q указан дважды", name)
		}
		seen[name] = true
		found := false
		for _, component := range available {
			if component.Name == name {
				result = append(result, component)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("неизвестный компонент %q; список: hotelctl components", name)
		}
	}
	return result, nil
}
