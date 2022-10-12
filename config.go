package main

import (
	"io/ioutil"
	"log"

	"gopkg.in/yaml.v2"
)

func parseConfig(path string) []Filter {
	yamlFile, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read conf.yaml #%v ", err)
	}

	var config struct {
		Filters []Filter `yaml:"filters"`
	}

	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	return config.Filters
}
