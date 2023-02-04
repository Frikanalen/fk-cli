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
	_ = viper.BindEnv("api")
	viper.SetDefault("API", "http://localhost:8080")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			newConfigFile := path.Join(os.Getenv("HOME"), ".frikanalen.yaml")
			err = viper.WriteConfigAs(newConfigFile)
			if err != nil {
				log.Fatalln("could not write configuration file %w", err)
			} else {
				log.Infoln("Created configuration file", newConfigFile)
			}
		} else {
			log.Fatalln("could not read config file, %w", err)
		}
	} else {
		log.Infoln("Loading configuration file", viper.ConfigFileUsed())
	}

	cmd.Execute()
}
