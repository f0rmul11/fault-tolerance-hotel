package config

import (
	"path/filepath"
	"testing"
)

func TestComponentSelectionAndKafkaDeployment(t *testing.T) {
	c := Config{
		Services: []Service{{
			Name:       "booking",
			Deployment: "booking",
		}},
	}
	selected, err := c.SelectComponents([]string{"booking", "kafka", "booking-postgres"})
	if err != nil || len(selected) != 3 || selected[1].Deployment != "booking-kafka" {
		t.Fatalf("incorrect component mapping: %+v, %v", selected, err)
	}
	for _, names := range [][]string{{"unknown"}, {"kafka", "kafka"}} {
		if _, err := c.SelectComponents(names); err == nil {
			t.Fatalf("accepted invalid names: %v", names)
		}
	}
}

func TestInfrastructurePortsCannotCollideWithApplicationPorts(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []bool{false, true} {
		c, err := Load(root, "")
		if err != nil {
			t.Fatal(err)
		}
		if node {
			c.Services[1].GRPC.Node = 30432
		} else {
			c.Services[1].GRPC.Host = 15432
		}
		if err := c.Validate(); err == nil {
			t.Fatal("accepted port collision")
		}
	}
}
