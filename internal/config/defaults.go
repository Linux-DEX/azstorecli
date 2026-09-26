package config

// Config is the full merged configuration for a run of azstorecli.
// Field tags match the YAML/env/flag keys used by koanf.
type Config struct {
	Version int `koanf:"version" yaml:"version"`

	Project struct {
		Name            string `koanf:"name" yaml:"name"`
		FunctionAppPath string `koanf:"functionAppPath" yaml:"functionAppPath"`
	} `koanf:"project" yaml:"project"`

	Azurite struct {
		Runtime              string `koanf:"runtime" yaml:"runtime"` // npx | global | docker | path
		WorkspaceMode        string `koanf:"workspaceMode" yaml:"workspaceMode"`
		WorkspaceDir         string `koanf:"workspaceDir" yaml:"workspaceDir"`
		BlobPort             int    `koanf:"blobPort" yaml:"blobPort"`
		QueuePort            int    `koanf:"queuePort" yaml:"queuePort"`
		TablePort            int    `koanf:"tablePort" yaml:"tablePort"`
		Loose                bool   `koanf:"loose" yaml:"loose"`
		SkipAPIVersionCheck  bool   `koanf:"skipApiVersionCheck" yaml:"skipApiVersionCheck"`
		Cert                 string `koanf:"cert" yaml:"cert"`
		Key                  string `koanf:"key" yaml:"key"`
	} `koanf:"azurite" yaml:"azurite"`

	Functions struct {
		Runtime string            `koanf:"runtime" yaml:"runtime"`
		Port    int               `koanf:"port" yaml:"port"`
		Watch   bool              `koanf:"watch" yaml:"watch"`
		Env     map[string]string `koanf:"env" yaml:"env"`
	} `koanf:"functions" yaml:"functions"`

	UI struct {
		Theme              string `koanf:"theme" yaml:"theme"`
		StartScreen        string `koanf:"startScreen" yaml:"startScreen"`
		ConfirmDestructive bool   `koanf:"confirmDestructive" yaml:"confirmDestructive"`
		PageSize           int    `koanf:"pageSize" yaml:"pageSize"`
	} `koanf:"ui" yaml:"ui"`

	Autostart []string `koanf:"autostart" yaml:"autostart"`
}

// Defaults returns the built-in configuration, before any file, env, or
// flag overrides are merged in.
func Defaults() Config {
	var c Config
	c.Version = 1

	c.Azurite.Runtime = "npx"
	c.Azurite.WorkspaceMode = "project"
	c.Azurite.WorkspaceDir = ".azstorecli/workspace"
	c.Azurite.BlobPort = 10000
	c.Azurite.QueuePort = 10001
	c.Azurite.TablePort = 10002
	c.Azurite.SkipAPIVersionCheck = true

	c.Functions.Runtime = "node"
	c.Functions.Port = 7071
	c.Functions.Watch = true
	c.Functions.Env = map[string]string{
		"AzureWebJobsStorage": "UseDevelopmentStorage=true",
	}

	c.UI.Theme = "dark"
	c.UI.StartScreen = "dashboard"
	c.UI.ConfirmDestructive = true
	c.UI.PageSize = 100

	c.Autostart = []string{"azurite", "functions"}

	return c
}

// AzuriteConnStr is Azurite's well-known devstore connection string. All
// three Azure SDK clients (azblob/azqueue/aztables) accept it directly.
const AzuriteConnStr = "DefaultEndpointsProtocol=http;" +
	"AccountName=devstoreaccount1;" +
	"AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq5ZUpgztYyxSGxrJOn3zJdCyMhwHDX0R8Cw2A==;" +
	"BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;" +
	"QueueEndpoint=http://127.0.0.1:10001/devstoreaccount1;" +
	"TableEndpoint=http://127.0.0.1:10002/devstoreaccount1;"
