package onyxia

import (
	"errors"
	"fmt"

	"github.com/expr-lang/expr"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chart"
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
	Filter ChartFilter `yaml:"filter"`
	Url    string      `yaml:"url"`
}

type ChartFilter func(chart.Chart) bool

func (f *ChartFilter) UnmarshalYAML(value *yaml.Node) error {
	prg, err := expr.Compile(value.Value, expr.Env(chart.Chart{}), expr.AsBool(), expr.WarnOnAny())
	if err != nil {
		return err
	}
	*f = func(c chart.Chart) bool {
		res, _ := expr.Run(prg, c)
		return res.(bool)
	}
	return nil
}

// FilterRelease filters out releases using the given filter
func (c Catalog) FilterRelease(r release.Release) bool {
	if c.Filter == nil {
		return true
	}
	if r.Chart == nil || r.Chart.Metadata == nil {
		return false
	}
	return c.Filter(*r.Chart)
}
