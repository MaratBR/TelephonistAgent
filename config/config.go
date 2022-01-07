package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaratBR/telephonist-supervisor/telephonist"
	"gopkg.in/yaml.v2"
)

var (
	configPath string
	config     Config
	Ptr        *Config
)

type Config struct {
	ApplicationSettings telephonist.SupervisorSettingsV1 `yaml:"application_settings"`
	ApplicationKey      string                           `yaml:"application_key"`
	TelephonistAddress  string                           `yaml:"telephonist_address"`
}

func (c *Config) HasApplicationKey() bool {
	return c.ApplicationKey != "" && strings.ToUpper(strings.ReplaceAll(c.ApplicationKey, " ", "")) != "INSERTYOURKEYHERE"
}

func SetConfigPath(path string) { configPath = path }

func GetConfigPath() string { return configPath }

func Init() error {
	configRoot := filepath.Dir(configPath)
	if _, err := os.Stat(configRoot); errors.Is(err, os.ErrNotExist) {
		err = os.MkdirAll(configRoot, os.ModePerm)
		if err != nil {
			return errors.New("failed to create a config directory at " + configRoot + ": " + err.Error())
		}
	}

	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		return Write()
	} else {
		err := Read()
		return err
	}
}

func Test() error {
	return ReadTo(&Config{})
}

func Read() error { return ReadTo(Ptr) }

func ReadTo(ptr *Config) error {
	f, err := os.Open(configPath)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(f)
	return decoder.Decode(ptr)
}

func Write() error { return WriteConfigTo(configPath, Ptr) }

func WriteConfigTo(file string, ptr *Config) error {
	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return err
	}
	encoder := yaml.NewEncoder(f)
	return encoder.Encode(ptr)
}

func init() {
	configPath = "/etc/telephonist/client-config.yaml"
	Ptr = &config

	config.ApplicationSettings.Tasks = []telephonist.TaskDescriptor{}
	config.TelephonistAddress = "http://127.0.0.1:5789/"
	config.ApplicationKey = "INSERT YOUR KEY HERE"
}
