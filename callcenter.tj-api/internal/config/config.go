package config

import (
    "os"
    "gopkg.in/yaml.v3"
)

type Config struct {
    HTTP struct {
        Addr string `yaml:"addr"`
    } `yaml:"http"`

    JWT struct {
        Secret     string `yaml:"secret"`
        TTLMinutes int    `yaml:"ttl_minutes"`
    } `yaml:"jwt"`

    DB struct {
        DSN string `yaml:"dsn"`
    } `yaml:"db"`
}

func Load() *Config {
    data, err := os.ReadFile("config.yaml")
    if err != nil {
        panic(err)
    }

    var c Config
    if err := yaml.Unmarshal(data, &c); err != nil {
        panic(err)
    }

    return &c
}
