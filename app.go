package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaratBR/telephonist-supervisor/config"
	"github.com/MaratBR/telephonist-supervisor/telephonist"
	"github.com/denisbrodbeck/machineid"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	log "github.com/inconshreveable/log15"
	"github.com/juju/fslock"
	"github.com/parnurzeal/gorequest"
	"github.com/urfave/cli/v2"
)

const (
	APPLICATION_NAME = "telephonist-supervisor"
	VERBOSE_F        = "verbose"
	ROOT_PATH_F      = "rootPath"
	CONFIG_PATH_F    = "config"
	SOCKET_F         = "socket"
)

var (
	ErrApplicationKeyIsMissing = errors.New("application key is missing")
)

type AppConfig struct {
	ApplicationSettings telephonist.SupervisorSettingsV1 `yaml:"application_settings"`
	ApplicationKey      string                           `yaml:"application_key"`
	TelephonistAddress  string                           `yaml:"telephonist_address"`
}

type App struct {
	cli.App
	fsLock          *fslock.Lock
	configFile      *ConfigFile
	config          AppConfig
	BackgroundTasks *BackgroundTasks
	instanceID      string
	machineID       string
}

func fatalOnErr(err error) {
	if err != nil {
		log.Crit(err.Error())
	}
}

func CreateNewApp() *App {
	app := &App{}
	app.Before = app.before
	app.Name = APPLICATION_NAME
	app.Usage = "Supervise multiple tasks and report all information to the Telephonist server"
	app.After = app.after
	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:  "secure",
			Value: false,
			Usage: "if set connects to API using https and wss (enforces HTTPS event if url is set to http://...)",
		},
		&cli.StringFlag{
			Name:  ROOT_PATH_F,
			Value: "/etc/telephonist",
			Usage: "path to the main configuration folder with client.yaml and client.lock",
		},
		&cli.StringFlag{
			Name:  SOCKET_F,
			Value: "/var/telephonist/client-daemon.socket",
			Usage: "sets the path to the file that will be used as a unix socket",
		},
		&cli.BoolFlag{
			Name:    VERBOSE_F,
			Aliases: []string{"v"},
			Value:   false,
			Usage:   "if set, outputs debug messages",
		},
	}
	app.Commands = []*cli.Command{
		{
			Name:   "test",
			Usage:  "tests if configuration is valid or not",
			Action: app.testAction,
		},
		{
			Name:   "refetch-config",
			Usage:  "tries to connect to the running instance of telephonist-supervisor and request a config reload",
			Action: app.refetchConfigAction,
		},
	}
	app.Action = app.action
	return app
}

func (a *App) refetchConfigAction(c *cli.Context) error {
	return nil
}

func (a *App) testAction(c *cli.Context) error {
	err := config.Test()
	if err != nil {
		return err
	}
	log.Info("config is OK")
	return nil
}

func (a *App) action(c *cli.Context) error {
	fatalOnErr(a.lockFile(c))
	var (
		err error
	)
	a.instanceID, err = a.getInstanceID(c)
	if err != nil {
		log.Crit("failed to read or write instance id", log.Ctx{"error": err.Error()})
	}
	a.machineID, err = machineid.ProtectedID(APPLICATION_NAME)
	if err != nil {
		log.Crit("failed to read machine id")
	}
	err = a.configFile.Load(&a.config)
	if err != nil {
		log.Crit(fmt.Sprintf("failed to load config file", log.Ctx{"file": a.configFile.filepath, "error": err.Error()}))
	}
	log.Debug("debug report", log.Ctx{"instance_id": a.instanceID, "machine_id": a.machineID, "config": a.configFile.filepath})

	err = a.prepareConfig()
	if err != nil {
		return fmt.Errorf("prepareConfig: %s", err.Error())
	}

	tasks := NewBackgroundTasks()
	tasks.AddFunction(BackgroundFunctionDescriptor{
		Restart:        true,
		RestartTimeout: time.Second * 2,
		Function:       a.runTelephonistClientWrapped,
		Name:           "runTelephonistClientWrapped",
	})
	tasks.AddFunction(BackgroundFunctionDescriptor{
		Restart:        true,
		RestartTimeout: time.Second * 2,
		Function:       a.runUnixSocketServerWrapped,
		Name:           "runUnixSocketServerWrapped",
	})

	err = tasks.Run(c)
	if err != nil {
		return fmt.Errorf("failed to run background tasks: %s", err.Error())
	}
	err = tasks.WaitForAll()
	if err != nil {
		return err
	}
	return nil
}

func (a *App) runUnixSocketServerWrapped(ctx *BackgroundFunctionContext) error {
	socketPath := ctx.Cli.String(SOCKET_F)
	dirname := filepath.Dir(socketPath)

	if err := os.MkdirAll(dirname, os.ModePerm); err != nil {
		return err
	}

	err := os.Remove(socketPath)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to remove unix socket file: %s", err.Error())
	}

	conn, err := net.Listen("unix", socketPath)
	if err != nil {
		return errors.New("in net.Listen: " + err.Error())
	}

	defer conn.Close()
	router := mux.NewRouter()

	router.HandleFunc("/telephonist/config-reload", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			rw.WriteHeader(405)
		} else {
			//reloadConfig()
		}
	})

	log.Info("starting unix socket server", log.Ctx{"file": socketPath})
	errChan := make(chan error, 1)
	go func() {
		errChan <- http.Serve(conn, router)
	}()

	select {
	case err = <-errChan:
		if err != nil {
			return errors.New("in http.Serve: " + err.Error())
		}
		return errors.New("http.Server unexpectedly exited without any errors")
	case <-ctx.CloseChannel:
		err := conn.Close()
		if err != nil {
			log.Error("failed to close connection", log.Ctx{"error": err.Error()})
		}
		return nil
	}
}

func (a *App) runTelephonistClientWrapped(ctx *BackgroundFunctionContext) error {
	u, err := url.Parse(a.config.TelephonistAddress)
	if err != nil {
		return fmt.Errorf("invalid URL format: %s", err.Error())
	}
	if u.Scheme != "" && u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("invalid URL schema, schema must be either http or https or empty (\"\")")
	}
	var client *telephonist.Client

	{
		u.Scheme = "https"
		_, _, errs := gorequest.New().Get(u.String()).End()
		if errs != nil {
			ctx.Logger.Info("failed to connect to the API through https, wiil assume http protocol is used")
			u.Scheme = "http"
		}
	}

	client, err = telephonist.NewClient(telephonist.ClientOptions{
		APIKey:     a.config.ApplicationKey,
		URL:        u,
		InstanceID: a.instanceID,
		MachineID:  a.machineID,
	})
	if err != nil {
		return fmt.Errorf("failed to create a client: %s", err.Error())
	}

	done := make(chan struct{})
	go func() {
		err = client.Run()
		done <- struct{}{}
	}()
	select {
	case <-ctx.CloseChannel:
		client.Stop()
	case <-done:
	}
	return err
}

func (a *App) prepareConfig() error {
	a.config.ApplicationKey = strings.Trim(a.config.ApplicationKey, " \t\n")
	if a.config.ApplicationKey == "" {
		return ErrApplicationKeyIsMissing
	}
	return nil
}

func (a *App) before(c *cli.Context) error {
	if c.Bool(VERBOSE_F) {

	}

	fatalOnErr(os.MkdirAll(c.String(ROOT_PATH_F), os.ModePerm))
	configPath := c.String(CONFIG_PATH_F)
	if configPath == "" {
		configPath = a.filenameHelper(c, "client-config.yaml")
	}
	a.configFile = NewConfigFile(configPath)
	// TODO check if we can open or write config file

	a.BackgroundTasks = NewBackgroundTasks()

	return nil
}

func (a *App) after(c *cli.Context) error {
	if a.fsLock != nil {
		if err := a.fsLock.Unlock(); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) filenameHelper(c *cli.Context, filename string) string {
	return filepath.Join(c.String(ROOT_PATH_F), filename)
}

func (a *App) lockFile(c *cli.Context) error {
	if a.fsLock != nil {
		return nil
	}
	lockFilepath := a.filenameHelper(c, "client.lock")
	a.fsLock = fslock.New(lockFilepath)
	return a.fsLock.LockWithTimeout(time.Second * 1)
}

func (a *App) getInstanceID(c *cli.Context) (string, error) {
	var id string
	path := a.filenameHelper(c, ".client-id")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			id, err = writeNewInstanceID(path)
			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	} else {
		id = strings.Trim(string(data), " \t\n")
	}

	if id == "" {
		id, err = writeNewInstanceID(path)
		if err != nil {
			return "", err
		}
	}
	return id, nil
}

func writeNewInstanceID(path string) (string, error) {
	id := uuid.NewString()
	err := os.WriteFile(path, []byte(id), os.ModePerm)
	if err != nil {
		return "", err
	}
	return id, nil
}
