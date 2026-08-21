package fk

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// DefaultEnvironment is the environment used when the configuration file
// does not name one.
const DefaultEnvironment = "local"

// Environment is a Frikanalen deployment the CLI can talk to. Each has its
// own API base URL and its own stored auth token, so switching between them
// does not mean logging in again.
type Environment struct {
	Name string
	API  string
	// LoggedIn reports whether a token is stored for this environment.
	LoggedIn bool
}

// builtinEnvironments are the deployments known without any configuration,
// in presentation order. The configuration file may override their URLs and
// may add environments of its own.
var builtinEnvironments = []struct{ name, api string }{
	{"local", "http://localhost:8000"},
	{"staging", "https://staging.frikanalen.no"},
	{"prod", "https://frikanalen.no"},
}

func builtinAPI(name string) string {
	for _, env := range builtinEnvironments {
		if env.name == name {
			return env.api
		}
	}
	return ""
}

func envKey(name, key string) string { return "environments." + name + "." + key }

// CurrentEnvironment returns the name of the active environment.
func CurrentEnvironment() string {
	if name := viper.GetString("environment"); name != "" {
		return name
	}
	return DefaultEnvironment
}

// EnvironmentAPI returns the API base URL for a named environment: the
// configured URL if there is one, otherwise the built-in default. It returns
// an empty string for an environment that is neither built in nor configured.
func EnvironmentAPI(name string) string {
	if api := viper.GetString(envKey(name, "api")); api != "" {
		return api
	}
	return builtinAPI(name)
}

// APIURL returns the base URL requests should go to. $FK_API overrides the
// active environment, for one-off runs against a server that has no
// environment of its own.
func APIURL() string {
	if api := os.Getenv("FK_API"); api != "" {
		return api
	}
	return EnvironmentAPI(CurrentEnvironment())
}

// StoredToken returns the auth token saved for the active environment, or an
// empty string if it has none.
func StoredToken() string {
	return viper.GetString(envKey(CurrentEnvironment(), "token"))
}

// SaveToken persists an auth token for the active environment.
func SaveToken(token string) error {
	viper.Set(envKey(CurrentEnvironment(), "token"), token)
	return viper.WriteConfig()
}

// KnownEnvironments lists the built-in environments merged with any defined
// in the configuration file, built-ins first.
func KnownEnvironments() []Environment {
	var envs []Environment
	seen := map[string]bool{}

	for _, builtin := range builtinEnvironments {
		seen[builtin.name] = true
		envs = append(envs, environment(builtin.name))
	}

	var extra []string
	for name := range viper.GetStringMap("environments") {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		envs = append(envs, environment(name))
	}

	return envs
}

func environment(name string) Environment {
	return Environment{
		Name:     name,
		API:      EnvironmentAPI(name),
		LoggedIn: viper.GetString(envKey(name, "token")) != "",
	}
}

// UseEnvironment makes name the active environment, defining it with the
// given API base URL when api is non-empty. An environment that is neither
// built in nor already configured must be given a URL.
func UseEnvironment(name, api string) error {
	if name == "" {
		return fmt.Errorf("environment name must not be empty")
	}
	if api == "" && EnvironmentAPI(name) == "" {
		return fmt.Errorf("unknown environment %q: pass --api to define it", name)
	}

	if api != "" {
		viper.Set(envKey(name, "api"), api)
	}
	viper.Set("environment", name)

	return viper.WriteConfig()
}

// MigrateLegacyConfig rewrites a pre-environments configuration file — one
// with a top-level "api"/"token" pair — into the environments layout, folding
// the old settings into whichever environment the URL belongs to. It reports
// whether it changed the file, and must run before the configuration is read.
// Files already using environments, and paths that do not exist, are left
// alone.
func MigrateLegacyConfig(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	var config map[string]any
	if err := yaml.Unmarshal(contents, &config); err != nil {
		return false, fmt.Errorf("parsing %s: %w", path, err)
	}
	if config == nil {
		return false, nil
	}
	if _, ok := config["environments"]; ok {
		return false, nil
	}

	api, _ := config["api"].(string)
	token, _ := config["token"].(string)
	if api == "" && token == "" {
		return false, nil
	}
	if api == "" {
		api = builtinAPI(DefaultEnvironment)
	}

	name := DefaultEnvironment
	for _, builtin := range builtinEnvironments {
		if builtin.api == api {
			name = builtin.name
			break
		}
	}

	delete(config, "api")
	delete(config, "token")
	config["environment"] = name
	config["environments"] = map[string]any{
		name: map[string]any{"api": api, "token": token},
	}

	migrated, err := yaml.Marshal(config)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, migrated, info.Mode().Perm()); err != nil {
		return false, err
	}

	return true, nil
}
