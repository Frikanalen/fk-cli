/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"github/frikanalen/fk-cli/cmd"
	fk "github/frikanalen/fk-cli/fk-client"

	"os"
	"path"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/viper"
)

func main() {
	configFile := path.Join(os.Getenv("HOME"), ".frikanalen.yaml")

	// Older versions stored a single api/token pair; fold that into the
	// environments layout before viper reads the file.
	migrated, err := fk.MigrateLegacyConfig(configFile)
	if err != nil {
		log.Fatalln("could not migrate configuration file:", err)
	}
	if migrated {
		log.Infoln("Migrated", configFile, "to per-environment configuration")
	}

	viper.SetConfigName(".frikanalen")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(os.Getenv("HOME"))

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			viper.Set("environment", fk.DefaultEnvironment)
			if err := viper.WriteConfigAs(configFile); err != nil {
				log.Fatalln("could not write configuration file %w", err)
			}
			log.Infoln("Created configuration file", configFile)
		} else {
			log.Fatalln("could not read config file, %w", err)
		}
	} else {
		log.Infoln("Loading configuration file", viper.ConfigFileUsed())
	}

	cmd.Execute()
}
