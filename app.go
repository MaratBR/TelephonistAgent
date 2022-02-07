package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaratBR/TelephonistAgent/logging"
	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/juju/fslock"
	"github.com/parnurzeal/gorequest"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

const (
	APPLICATION_NAME     = "TelephonistAgent"
	VERBOSE_F            = "verbose"
	CONFIG_FOLDER_PATH_F = "rootPath"
	CONFIG_PATH_F        = "config"
	AGENT_CONFIG_PATH_F  = "agent-config"
	SOCKET_F             = "socket"
)

var (
	ErrApplicationKeyIsMissing = errors.New("application key is missing")
	logger                     = logging.ChildLogger("app")
)

type AppConfig struct {
	ApplicationKey     string
	TelephonistAddress string
}

type App struct {
	cli.App
	fsLock *fslock.Lock

	appConfigFile     *ConfigFile
	appConfig         *AppConfig
	instanceID        string
	machineID         string
	telephonistClient *telephonist.Client
	executor          *ApplicationExecutor
	configFolderPath  string
}

func fatalOnErr(err error) {
	if err != nil {
		logger.Fatal(err.Error())
	}
}

func CreateNewApp() *App {
	app := &App{
		appConfig: &AppConfig{
			TelephonistAddress: "https://localhost:5789",
		},
	}
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
			Name:  CONFIG_FOLDER_PATH_F,
			Value: "/etc/telephonist-agent",
			Usage: "path to the main configuration folder with client.json and client.lock",
		},
		&cli.StringFlag{
			Name:  SOCKET_F,
			Value: "/var/telephonist-agent/client-daemon.socket",
			Usage: "sets the path to the file that will be used as a unix socket",
		},
		&cli.StringFlag{
			Name:  CONFIG_PATH_F,
			Value: "",
			Usage: "sets the path to the main config file",
		},
		&cli.StringFlag{
			Name:  AGENT_CONFIG_PATH_F,
			Value: "",
			Usage: "sets the path to the agent config file",
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
			Usage:  "tries to connect to the running instance of TelephonistAgent and request a config reload",
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
	err := a.appConfigFile.Load(&AppConfig{})
	if err != nil {
		return err
	}
	println("config is OK")
	return nil
}

func (a *App) before(c *cli.Context) error {
	if c.Bool(VERBOSE_F) {

	}

	fatalOnErr(os.MkdirAll(c.String(CONFIG_FOLDER_PATH_F), os.ModePerm))
	a.configFolderPath = c.String(CONFIG_FOLDER_PATH_F)
	configPath := c.String(CONFIG_PATH_F)
	if configPath == "" {
		configPath = a.filenameHelper("client-config.json")
	}
	a.appConfigFile = NewConfigFile(configPath)
	//TODO: check if we can open or write config file

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

// action runs the main action of this app (agent itself)
func (a *App) action(c *cli.Context) error {
	fatalOnErr(a.lockFile(c))
	var (
		err error
	)
	err = a.appConfigFile.LoadOrCreate(a.appConfig)
	if err != nil {
		logger.Fatal("failed to load config file",
			zap.String("error", err.Error()),
			zap.String("file", a.appConfigFile.filepath),
		)
	}
	logger.Debug("config file located",
		zap.String("config", a.appConfigFile.filepath),
	)
	logger.Debug("api url = " + a.appConfig.TelephonistAddress)

	err = a.prepareConfig()
	if err != nil {
		return fmt.Errorf("prepareConfig: %s", err.Error())
	}

	err = a.createTelephonistClient()
	if err != nil {
		return err
	}

	err = a.createExecutor()
	if err != nil {
		return err
	}

	err = a.executor.Start()
	if err != nil {
		return fmt.Errorf("failed to start executor: %s", err.Error())
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	a.executor.Stop()

	return nil
}

func (a *App) createTelephonistClient() error {
	u, err := url.Parse(a.appConfig.TelephonistAddress)
	if err != nil {
		return fmt.Errorf("invalid URL format: %s", err.Error())
	}
	if u.Scheme != "" && u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("invalid URL schema, schema must be either http or https or empty (\"\")")
	}

	{
		u.Scheme = "https"
		_, _, errs := gorequest.New().Get(u.String()).End()
		if errs != nil {
			logger.Warn("failed to connect to the API through https, will assume http protocol is used")
			u.Scheme = "http"
		}
	}

	a.telephonistClient, err = telephonist.NewClient(telephonist.ClientOptions{
		APIKey: a.appConfig.ApplicationKey,
		URL:    u,
	})
	if err != nil {
		return fmt.Errorf("failed to create a client: %s", err.Error())
	}
	return nil
}

func (a *App) createExecutor() error {
	if a.telephonistClient == nil {
		panic("telephonistClient is not set")
	}
	var err error
	a.executor, err = NewApplicationExecutor(ApplicationExecutorOptions{
		ConfigPath: a.filenameHelper("executor-config.json"),
		Client:     a.telephonistClient,
	})
	return err
}

// #region Utils

func (a *App) prepareConfig() error {
	a.appConfig.ApplicationKey = strings.Trim(a.appConfig.ApplicationKey, " \t\n")
	if a.appConfig.ApplicationKey == "" {
		return ErrApplicationKeyIsMissing
	}
	return nil
}

func (a *App) filenameHelper(filename string) string {
	return filepath.Join(a.configFolderPath, filename)
}

func (a *App) lockFile(c *cli.Context) error {
	if a.fsLock != nil {
		return nil
	}
	lockFilepath := a.filenameHelper("client.lock")
	a.fsLock = fslock.New(lockFilepath)
	return a.fsLock.LockWithTimeout(time.Second * 1)
}

// #endregion
