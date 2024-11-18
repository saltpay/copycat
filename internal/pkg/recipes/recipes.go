package recipes

type Recipe struct {
	Type        string `yaml:"type"`
	Name        string `yaml:"name"`
	DisplayName string `yaml:"displayName"`
}
