package main

import (
	"os"

	"gopkg.in/yaml.v2"
)

type ConfigFile struct {
	filepath string
}

func NewConfigFile(path string) *ConfigFile {
	return &ConfigFile{filepath: path}
}

func (cf *ConfigFile) Load(ptr interface{}) error {
	f, err := os.Open(cf.filepath)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	return decoder.Decode(ptr)
}

func (cf *ConfigFile) Write(ptr interface{}) error {
	f, err := os.OpenFile(cf.filepath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()
	encoder := yaml.NewEncoder(f)
	return encoder.Encode(ptr)
}
