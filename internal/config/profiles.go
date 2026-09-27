package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// ProfileType distinguishes how a profile authenticates.
type ProfileType string

const (
	// ProfileEmulator is Azurite on loopback with the well-known
	// devstore shared key.
	ProfileEmulator ProfileType = "emulator"
	// ProfileAzure is a real storage account.
	ProfileAzure ProfileType = "azure"
	// ProfileCustom is any other endpoint set (Azurite on a remote host,
	// a compatible third-party emulator).
	ProfileCustom ProfileType = "custom"
)

// AuthMode names how credentials are supplied.
type AuthMode string

const (
	AuthSharedKey AuthMode = "key"
	AuthSAS       AuthMode = "sas"
	AuthEntra     AuthMode = "entra"
)

// Profile is one connection target for the explorer screens.
type Profile struct {
	Name string      `yaml:"name"`
	Type ProfileType `yaml:"type"`
	Auth AuthMode    `yaml:"auth"`

	// ConnectionString short-circuits everything else when set; all
	// three SDKs parse it directly, Azurite's path-style URLs included.
	ConnectionString string `yaml:"connectionString,omitempty"`

	AccountName string `yaml:"accountName,omitempty"`
	AccountKey  string `yaml:"accountKey,omitempty"`
	SASToken    string `yaml:"sasToken,omitempty"`

	BlobEndpoint  string `yaml:"blobEndpoint,omitempty"`
	QueueEndpoint string `yaml:"queueEndpoint,omitempty"`
	TableEndpoint string `yaml:"tableEndpoint,omitempty"`

	// ReadOnly is a pointer so we can tell "user said false" from
	// "field omitted". Omitted means: emulator writable, everything
	// else read-only — the guardrail against muscle-memory deletes
	// against a real account.
	ReadOnly *bool `yaml:"readonly,omitempty"`
}

// ProfileStore is the on-disk profiles.yaml document.
type ProfileStore struct {
	Version  int       `yaml:"version"`
	Active   string    `yaml:"active"`
	Profiles []Profile `yaml:"profiles"`

	path         string
	InsecureMode bool // set at load time when the file is world/group readable
}

// AzuriteProfile is the built-in emulator profile, generated from the
// configured ports so an alt-port setup still gets a working default.
func AzuriteProfile(name string, blobPort, queuePort, tablePort int) Profile {
	return Profile{
		Name:             name,
		Type:             ProfileEmulator,
		Auth:             AuthSharedKey,
		ConnectionString: AzuriteConnStrPorts(blobPort, queuePort, tablePort),
		AccountName:      DevAccountName,
		AccountKey:       DevAccountKey,
		BlobEndpoint:     fmt.Sprintf("http://127.0.0.1:%d/%s", blobPort, DevAccountName),
		QueueEndpoint:    fmt.Sprintf("http://127.0.0.1:%d/%s", queuePort, DevAccountName),
		TableEndpoint:    fmt.Sprintf("http://127.0.0.1:%d/%s", tablePort, DevAccountName),
	}
}

// LoadProfiles reads profiles.yaml, seeding the default emulator profile
// when the file does not exist yet.
func LoadProfiles(path string, cfg Config) (*ProfileStore, error) {
	store := &ProfileStore{Version: 1, path: path}

	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		store.Profiles = []Profile{AzuriteProfile("azurite-local",
			cfg.Azurite.BlobPort, cfg.Azurite.QueuePort, cfg.Azurite.TablePort)}
		store.Active = "azurite-local"
		store.normalise()
		return store, nil
	case err != nil:
		return nil, err
	}

	if info, statErr := os.Stat(path); statErr == nil && runtime.GOOS != "windows" {
		// 0600 is the contract; anything looser means another local user
		// can read a real-cloud account key.
		if info.Mode().Perm()&0o077 != 0 {
			store.InsecureMode = true
		}
	}

	if err := yaml.Unmarshal(raw, store); err != nil {
		return nil, fmt.Errorf("profiles.yaml: %w", err)
	}
	store.path = path
	store.normalise()
	return store, store.validate()
}

// normalise fills in derived defaults. Real-cloud profiles are read-only
// unless the file says otherwise, because the cost of being wrong is a
// deleted production container.
func (s *ProfileStore) normalise() {
	for i := range s.Profiles {
		p := &s.Profiles[i]
		if p.Type == "" {
			p.Type = ProfileCustom
		}
		if p.Auth == "" {
			switch {
			case p.SASToken != "":
				p.Auth = AuthSAS
			case p.AccountKey != "" || p.ConnectionString != "":
				p.Auth = AuthSharedKey
			default:
				p.Auth = AuthEntra
			}
		}
	}
	if s.Active == "" && len(s.Profiles) > 0 {
		s.Active = s.Profiles[0].Name
	}
	if s.Version == 0 {
		s.Version = 1
	}
}

func (s *ProfileStore) validate() error {
	seen := map[string]bool{}
	for _, p := range s.Profiles {
		if p.Name == "" {
			return errors.New("profiles.yaml: a profile has no name")
		}
		if seen[p.Name] {
			return fmt.Errorf("profiles.yaml: duplicate profile %q", p.Name)
		}
		seen[p.Name] = true
	}
	if s.Active != "" && !seen[s.Active] {
		return fmt.Errorf("profiles.yaml: active profile %q is not defined", s.Active)
	}
	return nil
}

// Get returns a profile by name.
func (s *ProfileStore) Get(name string) (Profile, bool) {
	for _, p := range s.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// Current returns the active profile.
func (s *ProfileStore) Current() (Profile, bool) { return s.Get(s.Active) }

// Upsert adds or replaces a profile by name.
func (s *ProfileStore) Upsert(p Profile) {
	for i := range s.Profiles {
		if s.Profiles[i].Name == p.Name {
			s.Profiles[i] = p
			return
		}
	}
	s.Profiles = append(s.Profiles, p)
}

// Delete removes a profile, refusing to leave the store empty or to
// orphan the active pointer.
func (s *ProfileStore) Delete(name string) error {
	if len(s.Profiles) == 1 {
		return errors.New("cannot delete the last profile")
	}
	idx := -1
	for i, p := range s.Profiles {
		if p.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no profile named %q", name)
	}
	s.Profiles = append(s.Profiles[:idx], s.Profiles[idx+1:]...)
	if s.Active == name {
		s.Active = s.Profiles[0].Name
	}
	return nil
}

// Activate switches the active profile.
func (s *ProfileStore) Activate(name string) error {
	if _, ok := s.Get(name); !ok {
		return fmt.Errorf("no profile named %q", name)
	}
	s.Active = name
	return nil
}

// Save writes profiles.yaml atomically with mode 0600.
func (s *ProfileStore) Save() error {
	if s.path == "" {
		return errors.New("profile store has no path")
	}
	if err := s.validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	if err := util.WriteAtomic(s.path, data, 0o600); err != nil {
		return err
	}
	s.InsecureMode = false
	return nil
}

// Path returns the file this store was loaded from.
func (s *ProfileStore) Path() string { return s.path }

// ConnString renders a connection string for a profile, building one
// from the endpoint fields when none was supplied verbatim.
func (p Profile) ConnString() string {
	if p.ConnectionString != "" {
		return p.ConnectionString
	}
	var b strings.Builder
	scheme := "https"
	if strings.HasPrefix(p.BlobEndpoint, "http://") {
		scheme = "http"
	}
	fmt.Fprintf(&b, "DefaultEndpointsProtocol=%s;", scheme)
	if p.AccountName != "" {
		fmt.Fprintf(&b, "AccountName=%s;", p.AccountName)
	}
	if p.AccountKey != "" {
		fmt.Fprintf(&b, "AccountKey=%s;", p.AccountKey)
	}
	if p.SASToken != "" {
		fmt.Fprintf(&b, "SharedAccessSignature=%s;", strings.TrimPrefix(p.SASToken, "?"))
	}
	if p.BlobEndpoint != "" {
		fmt.Fprintf(&b, "BlobEndpoint=%s;", p.BlobEndpoint)
	}
	if p.QueueEndpoint != "" {
		fmt.Fprintf(&b, "QueueEndpoint=%s;", p.QueueEndpoint)
	}
	if p.TableEndpoint != "" {
		fmt.Fprintf(&b, "TableEndpoint=%s;", p.TableEndpoint)
	}
	return b.String()
}

var secretRe = regexp.MustCompile(`(?i)(AccountKey=|SharedAccessSignature=|sig=)([^;&]+)`)

// Redact masks secrets in a connection string so it can be shown in the
// UI or copied to the clipboard without leaking a key.
func Redact(s string) string {
	return secretRe.ReplaceAllString(s, "${1}"+strings.Repeat("*", 8))
}

// IsEmulator reports whether a profile points at a local emulator, which
// is what gates destructive operations being allowed by default.
func (p Profile) IsEmulator() bool { return p.Type == ProfileEmulator }

// IsReadOnly reports whether mutating operations must be refused.
func (p Profile) IsReadOnly() bool {
	if p.ReadOnly != nil {
		return *p.ReadOnly
	}
	return !p.IsEmulator()
}

// SetReadOnly writes the flag so a later Save persists the user's choice.
func (p *Profile) SetReadOnly(v bool) { p.ReadOnly = &v }
