package config

import "fmt"

// Config is the full merged configuration for a run of azstorecli.
// Field tags match the YAML/env/flag keys used by koanf.
type Config struct {
	Version int `koanf:"version" yaml:"version"`

	Project struct {
		Name            string `koanf:"name" yaml:"name"`
		FunctionAppPath string `koanf:"functionAppPath" yaml:"functionAppPath"`
	} `koanf:"project" yaml:"project"`

	Azurite struct {
		Runtime             string   `koanf:"runtime" yaml:"runtime"` // npx | global | docker | path
		Path                string   `koanf:"path" yaml:"path"`       // used when runtime == path
		Image               string   `koanf:"image" yaml:"image"`     // used when runtime == docker
		WorkspaceMode       string   `koanf:"workspaceMode" yaml:"workspaceMode"`
		WorkspaceDir        string   `koanf:"workspaceDir" yaml:"workspaceDir"`
		BlobPort            int      `koanf:"blobPort" yaml:"blobPort"`
		QueuePort           int      `koanf:"queuePort" yaml:"queuePort"`
		TablePort           int      `koanf:"tablePort" yaml:"tablePort"`
		Services            []string `koanf:"services" yaml:"services"` // blob, queue, table
		Loose               bool     `koanf:"loose" yaml:"loose"`
		SkipAPIVersionCheck bool     `koanf:"skipApiVersionCheck" yaml:"skipApiVersionCheck"`
		Silent              bool     `koanf:"silent" yaml:"silent"`
		Debug               bool     `koanf:"debug" yaml:"debug"`
		Cert                string   `koanf:"cert" yaml:"cert"`
		Key                 string   `koanf:"key" yaml:"key"`
		ExtraArgs           []string `koanf:"extraArgs" yaml:"extraArgs"`
	} `koanf:"azurite" yaml:"azurite"`

	Functions struct {
		Enabled   bool              `koanf:"enabled" yaml:"enabled"`
		Runtime   string            `koanf:"runtime" yaml:"runtime"`
		Port      int               `koanf:"port" yaml:"port"`
		Watch     bool              `koanf:"watch" yaml:"watch"`
		Verbose   bool              `koanf:"verbose" yaml:"verbose"`
		Env       map[string]string `koanf:"env" yaml:"env"`
		ExtraArgs []string          `koanf:"extraArgs" yaml:"extraArgs"`
	} `koanf:"functions" yaml:"functions"`

	Seed struct {
		AutoApply bool   `koanf:"autoApply" yaml:"autoApply"`
		File      string `koanf:"file" yaml:"file"`
	} `koanf:"seed" yaml:"seed"`

	UI struct {
		Theme              string `koanf:"theme" yaml:"theme"`
		StartScreen        string `koanf:"startScreen" yaml:"startScreen"`
		ConfirmDestructive bool   `koanf:"confirmDestructive" yaml:"confirmDestructive"`
		PageSize           int    `koanf:"pageSize" yaml:"pageSize"`
		PreviewLimitBytes  int64  `koanf:"previewLimitBytes" yaml:"previewLimitBytes"`
		LogCapacity        int    `koanf:"logCapacity" yaml:"logCapacity"`
	} `koanf:"ui" yaml:"ui"`

	Autostart []string `koanf:"autostart" yaml:"autostart"`

	// projectDir is the directory .azstorecli/ was found in. It is not
	// serialised — it is discovered at load time and every relative path
	// in the file resolves against it.
	projectDir string `koanf:"-" yaml:"-"`
}

// Defaults returns the built-in configuration, before any file, env, or
// flag overrides are merged in.
func Defaults() Config {
	var c Config
	c.Version = 1

	c.Project.FunctionAppPath = "."

	c.Azurite.Runtime = "npx"
	c.Azurite.Image = "mcr.microsoft.com/azure-storage/azurite"
	c.Azurite.WorkspaceMode = "project"
	c.Azurite.WorkspaceDir = ".azstorecli/workspace"
	c.Azurite.BlobPort = 10000
	c.Azurite.QueuePort = 10001
	c.Azurite.TablePort = 10002
	c.Azurite.Services = []string{"blob", "queue", "table"}
	// The Go SDK sends a newer x-ms-version than older Azurite builds
	// accept, and the failure is an opaque 400. Skipping the check by
	// default trades a theoretical mismatch for a real, common one.
	c.Azurite.SkipAPIVersionCheck = true

	c.Functions.Enabled = true
	c.Functions.Runtime = "node"
	c.Functions.Port = 7071
	c.Functions.Watch = true
	c.Functions.Env = map[string]string{
		"AzureWebJobsStorage": "UseDevelopmentStorage=true",
	}

	c.Seed.File = ".azstorecli/seed/containers.yaml"

	c.UI.Theme = "dark"
	c.UI.StartScreen = "dashboard"
	c.UI.ConfirmDestructive = true
	c.UI.PageSize = 100
	c.UI.PreviewLimitBytes = 2 << 20 // 2 MB, then stream the first 256 KB
	c.UI.LogCapacity = 10_000

	c.Autostart = []string{"azurite", "functions"}

	return c
}

// Well-known Azurite development credentials. These are published by
// Microsoft and identical on every install, so they are not a secret —
// but no other credential may ever be hardcoded (see the AppSec rule).
const (
	DevAccountName = "devstoreaccount1"
	// Azurite's published development key. A different key is rejected
	// with 403, so a create never lands and the sidebar stays empty.
	DevAccountKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
)

// AzuriteConnStr is the devstore connection string on the default ports.
const AzuriteConnStr = "DefaultEndpointsProtocol=http;" +
	"AccountName=" + DevAccountName + ";" +
	"AccountKey=" + DevAccountKey + ";" +
	"BlobEndpoint=http://127.0.0.1:10000/" + DevAccountName + ";" +
	"QueueEndpoint=http://127.0.0.1:10001/" + DevAccountName + ";" +
	"TableEndpoint=http://127.0.0.1:10002/" + DevAccountName + ";"

// AzuriteConnStrPorts builds the devstore connection string for a
// non-default port trio. azblob.NewClientFromConnectionString handles
// the path-style URL (account in the path, not the subdomain) that
// Azurite uses, so no special-casing is needed downstream.
func AzuriteConnStrPorts(blob, queue, table int) string {
	return fmt.Sprintf(
		"DefaultEndpointsProtocol=http;AccountName=%s;AccountKey=%s;"+
			"BlobEndpoint=http://127.0.0.1:%d/%s;"+
			"QueueEndpoint=http://127.0.0.1:%d/%s;"+
			"TableEndpoint=http://127.0.0.1:%d/%s;",
		DevAccountName, DevAccountKey,
		blob, DevAccountName, queue, DevAccountName, table, DevAccountName)
}
