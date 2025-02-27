package onyxia

import (
	"errors"
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/release"
)

type Catalogs map[string]Catalog

// Validate checks whether all Catalogs have their URL set.
func (cs Catalogs) Validate() error {
	var err error
	for name, c := range cs {
		if c.Url == "" {
			err = errors.Join(err, fmt.Errorf("catalog %s missing url", name))
		}
	}
	return err
}

// UnmarshalText lets us automatically create the catalog from a environment
// variable YAML string.
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

// FilterRelease filters out releases if:
// - Catalog.Include is given, and the release is not in it
// - Catalog.Exclude is given, and the release is in it
func (c Catalog) FilterRelease(r release.Release) bool {
	if c.Include != nil {
		if !slices.Contains(c.Include, r.Chart.Metadata.Name) {
			return false
		}
	}

	if c.Exclude != nil {
		if slices.Contains(c.Exclude, r.Chart.Metadata.Name) {
			return false
		}
	}

	return true
}
