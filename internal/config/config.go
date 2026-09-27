package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
	goyaml "gopkg.in/yaml.v3"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// Load merges configuration in the order defined in docs/ARCHITECTURE.md
// §4.4, last wins:
//
//	defaults → ~/.config/azstorecli/config.yaml → ./.azstorecli/config.yaml
//	→ AZSTORE_* env → command-line flags
//
// Missing files are not errors: a fresh install has neither file, and
// Defaults() already covers that case.
func Load(flags *pflag.FlagSet) (Config, error) {
	k := koanf.New(".")

	if err := k.Load(structs.Provider(Defaults(), "koanf"), nil); err != nil {
		return Config{}, err
	}

	if err := loadFileIfPresent(k, GlobalConfigPath()); err != nil {
		return Config{}, fmt.Errorf("global config: %w", err)
	}

	projectPath, projectDir, err := FindProject()
	if err != nil {
		return Config{}, err
	}
	if projectPath != "" {
		if err := loadFileIfPresent(k, projectPath); err != nil {
			return Config{}, fmt.Errorf("project config: %w", err)
		}
	}

	if err := k.Load(env.Provider("AZSTORE_", ".", normalizeEnv), nil); err != nil {
		return Config{}, err
	}

	// posflag only overrides keys whose flag was actually Changed, so
	// an unset flag's zero value never clobbers a file setting.
	if flags != nil {
		if err := k.Load(posflag.Provider(flags, ".", k), nil); err != nil {
			return Config{}, err
		}
	}

	var cfg Config
	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return Config{}, err
	}
	cfg.projectDir = projectDir
	if cfg.projectDir == "" {
		// Outside a project, relative paths resolve against the cwd.
		if wd, err := os.Getwd(); err == nil {
			cfg.projectDir = wd
		}
	}
	if cfg.Project.Name == "" {
		cfg.Project.Name = filepath.Base(cfg.projectDir)
	}
	return cfg, cfg.Validate()
}

func loadFileIfPresent(k *koanf.Koanf, path string) error {
	err := k.Load(file.Provider(path), yaml.Parser())
	if err != nil && !isNotExist(err) {
		return err
	}
	return nil
}

// normalizeEnv converts AZSTORE_UI_THEME to ui.theme. Keys are matched
// case-insensitively downstream, so blobPort and blobport both resolve.
//
// Nested maps with case-sensitive keys (functions.env) cannot round-trip
// through this transform; set those in config.yaml, not the environment.
func normalizeEnv(s string) string {
	s = strings.TrimPrefix(s, "AZSTORE_")
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, "_", ".")
}

// isNotExist reports whether err is (or wraps) a file-not-found error.
func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

// Validate catches the configuration mistakes that would otherwise fail
// far from their cause — a port collision surfacing as an Azurite bind
// error, or a bad runtime name as "executable not found".
func (c Config) Validate() error {
	var errs []error

	ports := map[string]int{
		"blob":      c.Azurite.BlobPort,
		"queue":     c.Azurite.QueuePort,
		"table":     c.Azurite.TablePort,
		"functions": c.Functions.Port,
	}
	seen := map[int]string{}
	for name, p := range ports {
		if p < 1 || p > 65535 {
			errs = append(errs, fmt.Errorf("%s port %d is out of range", name, p))
			continue
		}
		if other, dup := seen[p]; dup {
			errs = append(errs, fmt.Errorf("%s and %s are both configured on port %d", other, name, p))
			continue
		}
		seen[p] = name
	}

	switch c.Azurite.Runtime {
	case "npx", "global", "docker", "path":
	default:
		errs = append(errs, fmt.Errorf("azurite.runtime %q must be npx, global, docker, or path", c.Azurite.Runtime))
	}
	if c.Azurite.Runtime == "path" && c.Azurite.Path == "" {
		errs = append(errs, errors.New("azurite.runtime is \"path\" but azurite.path is empty"))
	}

	switch c.Azurite.WorkspaceMode {
	case "project", "global", "temp":
	default:
		errs = append(errs, fmt.Errorf("azurite.workspaceMode %q must be project, global, or temp", c.Azurite.WorkspaceMode))
	}

	// HTTPS needs both halves; one alone silently falls back to HTTP and
	// then every SDK client fails its TLS handshake instead.
	if (c.Azurite.Cert == "") != (c.Azurite.Key == "") {
		errs = append(errs, errors.New("azurite.cert and azurite.key must be set together"))
	}

	for _, svc := range c.Azurite.Services {
		switch svc {
		case "blob", "queue", "table":
		default:
			errs = append(errs, fmt.Errorf("azurite.services has unknown service %q", svc))
		}
	}
	if len(c.Azurite.Services) == 0 {
		errs = append(errs, errors.New("azurite.services is empty — nothing would be started"))
	}

	if c.UI.PageSize < 1 || c.UI.PageSize > 5000 {
		errs = append(errs, fmt.Errorf("ui.pageSize %d must be between 1 and 5000", c.UI.PageSize))
	}

	for _, s := range c.Autostart {
		if s != "azurite" && s != "functions" {
			errs = append(errs, fmt.Errorf("autostart has unknown service %q", s))
		}
	}

	return errors.Join(errs...)
}

// ProjectDir returns the directory the project config was found in, or
// the working directory when running outside a project.
func (c Config) ProjectDir() string { return c.projectDir }

// SetProjectDir is used by `azstore init` and by tests to point a
// freshly built Config at a directory.
func (c *Config) SetProjectDir(dir string) { c.projectDir = dir }

// Resolve turns a possibly-relative config path into an absolute one,
// anchored at the project directory rather than the process working
// directory — running `azstore` from a subfolder must not relocate the
// Azurite workspace.
func (c Config) Resolve(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.projectDir, path)
}

// FunctionAppDir is the absolute path to the Functions project root.
func (c Config) FunctionAppDir() string {
	if c.Project.FunctionAppPath == "" {
		return c.projectDir
	}
	return c.Resolve(c.Project.FunctionAppPath)
}

// HasService reports whether an Azurite service is enabled.
func (c Config) HasService(name string) bool {
	for _, s := range c.Azurite.Services {
		if s == name {
			return true
		}
	}
	return false
}

// Scheme is http unless a cert/key pair was configured.
func (c Config) Scheme() string {
	if c.Azurite.Cert != "" && c.Azurite.Key != "" {
		return "https"
	}
	return "http"
}

// EmulatorProfile builds the emulator connection profile matching this
// configuration's ports.
func (c Config) EmulatorProfile(name string) Profile {
	return AzuriteProfile(name, c.Azurite.BlobPort, c.Azurite.QueuePort, c.Azurite.TablePort)
}

// Save writes the project config to .azstorecli/config.yaml atomically.
func (c Config) Save() error {
	data, err := goyaml.Marshal(c)
	if err != nil {
		return err
	}
	return util.WriteAtomic(ProjectConfigPath(c.projectDir), data, 0o644)
}
