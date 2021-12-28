/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

*/
package main

import (
	"github/frikanalen/fk-cli/cmd"

	"os"
	"path"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/viper"
)

func main() {
	viper.SetConfigName(".frikanalen")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(os.Getenv("HOME"))
        viper.SetEnvPrefix("fk")
        viper.BindEnv("api")
	viper.SetDefault("API", "http://localhost:8000")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			err = viper.WriteConfigAs(path.Join(os.Getenv("HOME"), ".frikanalen.yaml"))
			if err != nil {
				log.Fatalf("could not write configuration file %w", err)
			}
		} else {
			log.Fatalf("could not read config file, %w", err)
		}
	}

	cmd.Execute()
}
