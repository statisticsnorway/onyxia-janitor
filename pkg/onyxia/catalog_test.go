package onyxia_test

import (
	"onyxia-janitor/pkg/onyxia"
	"testing"

	"go.yaml.in/yaml/v3"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	release "helm.sh/helm/v4/pkg/release/v1"
)

func TestUnmarshalCatalogs(t *testing.T) {
	tests := []struct {
		Description string
		Text        string
		Releases    []release.Release
		Expected    []bool
	}{
		{
			Description: "only vscode-python",
			Text:        `{ "dapla": { "filter": "Metadata.Name startsWith \"vscode\"" } }`,
			Releases: []release.Release{
				{
					Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "vscode-python"}},
				},
				{
					Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "jupyter"}},
				},
				{
					Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "rstudio"}},
				},
			},
			Expected: []bool{
				true,
				false,
				false,
			},
		},
	}

	for _, tt := range tests {
		catalogs := make(onyxia.Catalogs)
		if err := yaml.Unmarshal([]byte(tt.Text), catalogs); err != nil {
			t.Fatal(err)
		}
		if _, ok := (catalogs)["dapla"]; !ok {
			t.Fatalf("no dapla catalog: %v", catalogs)
		}
		for i := range tt.Releases {
			if res := (catalogs)["dapla"].FilterRelease(tt.Releases[i]); res != tt.Expected[i] {
				t.Errorf("FilterRelease(%s) = %v, expected %v, included=%v", tt.Releases[i].Name, res, tt.Expected[i], tt.Text)
			}
		}
	}

}
