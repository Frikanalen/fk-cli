package fk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// writeConfig points viper at a fresh config file containing the given YAML,
// returning its path. Viper's global state is reset for each test.
func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".frikanalen.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	return path
}

func readConfig(t *testing.T, path string) map[string]any {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(contents, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestCurrentEnvironmentDefaults(t *testing.T) {
	writeConfig(t, "")

	if got := CurrentEnvironment(); got != DefaultEnvironment {
		t.Errorf("CurrentEnvironment() = %q, want %q", got, DefaultEnvironment)
	}
	if got := APIURL(); got != "http://localhost:8000" {
		t.Errorf("APIURL() = %q, want the built-in local URL", got)
	}
}

func TestPerEnvironmentTokens(t *testing.T) {
	writeConfig(t, `environment: staging
environments:
  local:
    token: local-token
  staging:
    token: staging-token
`)

	if got := APIURL(); got != "https://staging.frikanalen.no" {
		t.Errorf("APIURL() = %q, want the built-in staging URL", got)
	}
	if got := StoredToken(); got != "staging-token" {
		t.Errorf("StoredToken() = %q, want staging-token", got)
	}

	if err := UseEnvironment("local", ""); err != nil {
		t.Fatalf("UseEnvironment: %v", err)
	}
	if got := StoredToken(); got != "local-token" {
		t.Errorf("after switching, StoredToken() = %q, want local-token", got)
	}
}

func TestConfiguredAPIOverridesBuiltin(t *testing.T) {
	writeConfig(t, `environment: prod
environments:
  prod:
    api: https://mirror.example.com
`)

	if got := APIURL(); got != "https://mirror.example.com" {
		t.Errorf("APIURL() = %q, want the configured URL", got)
	}
}

func TestAPIURLEnvironmentVariableWins(t *testing.T) {
	writeConfig(t, "environment: prod\n")
	t.Setenv("FK_API", "http://elsewhere:9000")

	if got := APIURL(); got != "http://elsewhere:9000" {
		t.Errorf("APIURL() = %q, want $FK_API to win", got)
	}
}

func TestUseEnvironmentPersists(t *testing.T) {
	path := writeConfig(t, "")

	if err := UseEnvironment("staging", ""); err != nil {
		t.Fatalf("UseEnvironment: %v", err)
	}

	config := readConfig(t, path)
	if config["environment"] != "staging" {
		t.Errorf("environment = %v, want staging", config["environment"])
	}
	// A built-in environment needs no api key written out for it.
	if _, ok := config["environments"]; ok {
		t.Errorf("unexpected environments block for a built-in: %v", config["environments"])
	}
}

func TestUseEnvironmentUnknownNeedsAPI(t *testing.T) {
	writeConfig(t, "")

	if err := UseEnvironment("bogus", ""); err == nil {
		t.Fatal("UseEnvironment(bogus) succeeded, want an error demanding --api")
	}

	if err := UseEnvironment("bogus", "http://bogus.example.com"); err != nil {
		t.Fatalf("UseEnvironment with an api URL: %v", err)
	}
	if got := APIURL(); got != "http://bogus.example.com" {
		t.Errorf("APIURL() = %q, want the newly defined environment's URL", got)
	}
}

func TestKnownEnvironmentsIncludesConfiguredOnes(t *testing.T) {
	writeConfig(t, `environment: dev
environments:
  dev:
    api: http://dev.example.com
    token: dev-token
`)

	envs := KnownEnvironments()
	if len(envs) != 4 {
		t.Fatalf("got %d environments, want the 3 built-ins plus dev: %+v", len(envs), envs)
	}
	dev := envs[3]
	if dev.Name != "dev" || dev.API != "http://dev.example.com" || !dev.LoggedIn {
		t.Errorf("unexpected dev environment: %+v", dev)
	}
	if envs[0].Name != "local" || envs[0].LoggedIn {
		t.Errorf("unexpected local environment: %+v", envs[0])
	}
}

func TestMigrateLegacyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".frikanalen.yaml")
	if err := os.WriteFile(path, []byte("api: https://frikanalen.no\ntoken: legacy\nkeep: me\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrated, err := MigrateLegacyConfig(path)
	if err != nil {
		t.Fatalf("MigrateLegacyConfig: %v", err)
	}
	if !migrated {
		t.Fatal("MigrateLegacyConfig reported no change, want a migration")
	}

	config := readConfig(t, path)
	if config["environment"] != "prod" {
		t.Errorf("environment = %v, want prod (matched by URL)", config["environment"])
	}
	if _, ok := config["api"]; ok {
		t.Error("legacy api key survived the migration")
	}
	if _, ok := config["token"]; ok {
		t.Error("legacy token key survived the migration")
	}
	if config["keep"] != "me" {
		t.Error("migration dropped an unrelated key")
	}

	envs, ok := config["environments"].(map[string]any)
	if !ok {
		t.Fatalf("environments = %v, want a map", config["environments"])
	}
	prod, ok := envs["prod"].(map[string]any)
	if !ok {
		t.Fatalf("environments.prod = %v, want a map", envs["prod"])
	}
	if prod["api"] != "https://frikanalen.no" || prod["token"] != "legacy" {
		t.Errorf("unexpected prod environment: %v", prod)
	}

	// Running again must leave the migrated file alone.
	migrated, err = MigrateLegacyConfig(path)
	if err != nil {
		t.Fatalf("second MigrateLegacyConfig: %v", err)
	}
	if migrated {
		t.Error("MigrateLegacyConfig migrated an already-migrated file")
	}
}

func TestMigrateLegacyConfigUnknownURLBecomesDefaultEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".frikanalen.yaml")
	if err := os.WriteFile(path, []byte("api: http://vm.local:8000\ntoken: legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateLegacyConfig(path); err != nil {
		t.Fatalf("MigrateLegacyConfig: %v", err)
	}

	config := readConfig(t, path)
	if config["environment"] != DefaultEnvironment {
		t.Errorf("environment = %v, want %q", config["environment"], DefaultEnvironment)
	}
	envs := config["environments"].(map[string]any)
	local := envs[DefaultEnvironment].(map[string]any)
	if local["api"] != "http://vm.local:8000" {
		t.Errorf("api = %v, want the URL carried over from the legacy config", local["api"])
	}
}

func TestMigrateLegacyConfigSkipsMissingAndModernFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	if migrated, err := MigrateLegacyConfig(missing); err != nil || migrated {
		t.Errorf("MigrateLegacyConfig(missing) = %v, %v; want false, nil", migrated, err)
	}

	modern := filepath.Join(t.TempDir(), ".frikanalen.yaml")
	contents := "environment: local\nenvironments:\n  local:\n    token: t\n"
	if err := os.WriteFile(modern, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if migrated, err := MigrateLegacyConfig(modern); err != nil || migrated {
		t.Errorf("MigrateLegacyConfig(modern) = %v, %v; want false, nil", migrated, err)
	}
}
