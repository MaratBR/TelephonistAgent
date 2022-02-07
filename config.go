package main

import (
	"encoding/json"
	"os"
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
	decoder := json.NewDecoder(f)
	return decoder.Decode(ptr)
}

func (cf *ConfigFile) LoadOrCreate(ptr interface{}) error {
	if ptr == nil {
		panic("Cannot call LoadOrCreate with nil pointer")
	}
	err := cf.Load(ptr)
	if os.IsNotExist(err) {
		err = cf.Write(ptr)
	}
	return err
}

func (cf *ConfigFile) Write(ptr interface{}) error {
	f, err := os.OpenFile(cf.filepath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "    ")
	return encoder.Encode(ptr)
}
