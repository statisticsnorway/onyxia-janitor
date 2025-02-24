package onyxia

import (
	"slices"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/release"
)

type Catalogs map[string]Catalog

func (c *Catalogs) UnmarshalText(data []byte) error {
	return yaml.Unmarshal(data, c)
}

func (c Catalogs) Exists(catalogName string) bool {
	_, ok := c[catalogName]
	return ok
}

type Catalog struct {
	Include []string `yaml:"include" json:"include"`
	Exclude []string `yaml:"exclude"`
	Url     string   `yaml:"url"`
}

func (c Catalog) FilterRelease(r release.Release) bool {
	if c.Include != nil {
		if !slices.Contains(c.Include, r.Chart.Name()) {
			return false
		}
	}

	if c.Exclude != nil {
		if slices.Contains(c.Exclude, r.Chart.Name()) {
			return false
		}
	}

	return true
}
