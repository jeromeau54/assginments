package main

import (
	"github.com/spf13/viper"
)

type Config struct {
	Interface   string
	Port        string
	TargetProto string
	TargetHost  string
	TargetPort  string
	TargetPath  string
	RedisHost   string
	RedisPort   string
}

func loadConfig() Config {
	viper.SetDefault("INTERFACE", "0.0.0.0")
	viper.SetDefault("PORT", "8000")
	viper.SetDefault("TARGET_PROTO", "http")
	viper.SetDefault("TARGET_HOST", "gateway")
	viper.SetDefault("TARGET_PORT", "8000")
	viper.SetDefault("TARGET_PATH", "/healthz")
	viper.SetDefault("REDIS_HOST", "redis")
	viper.SetDefault("REDIS_PORT", "6379")

	viper.AutomaticEnv()

	return Config{
		Interface:   viper.GetString("INTERFACE"),
		Port:        viper.GetString("PORT"),
		TargetProto: viper.GetString("TARGET_PROTO"),
		TargetHost:  viper.GetString("TARGET_HOST"),
		TargetPort:  viper.GetString("TARGET_PORT"),
		TargetPath:  viper.GetString("TARGET_PATH"),
		RedisHost:   viper.GetString("REDIS_HOST"),
		RedisPort:   viper.GetString("REDIS_PORT"),
	}
}
