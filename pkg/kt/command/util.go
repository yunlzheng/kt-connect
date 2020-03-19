package command

import (
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alibaba/kt-connect/pkg/kt"

	"github.com/alibaba/kt-connect/pkg/kt/options"
	"github.com/alibaba/kt-connect/pkg/kt/util"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli"
)

// NewCommands return new Connect Command
func NewCommands(kt kt.CliInterface, action ActionInterface, options *options.DaemonOptions) []cli.Command {
	return []cli.Command{
		newRunCommand(kt, options, action),
		newConnectCommand(kt, options, action),
		newExchangeCommand(kt, options, action),
		newMeshCommand(kt, options, action),
		newDashboardCommand(kt, options, action),
		NewCheckCommand(kt, options, action),
	}
}

// SetUpWaitingChannel registry waiting channel
func SetUpWaitingChannel() (ch chan os.Signal) {
	ch = make(chan os.Signal)
	signal.Notify(ch, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	return
}

// SetUpCloseHandler registry close handeler
func SetUpCloseHandler(cli kt.CliInterface, options *options.DaemonOptions) (ch chan os.Signal) {
	ch = make(chan os.Signal)
	// see https://en.wikipedia.org/wiki/Signal_(IPC)
	signal.Notify(ch, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL)
	go func() {
		<-ch
		log.Info().Msgf("- Terminal And Clean Workspace\n")
		CleanupWorkspace(cli, options)
		log.Info().Msgf("- Successful Clean Up Workspace\n")
		os.Exit(0)
	}()
	return
}

// CleanupWorkspace clean workspace
func CleanupWorkspace(cli kt.CliInterface, options *options.DaemonOptions) {
	log.Info().Msgf("- start Clean Workspace\n")
	if _, err := os.Stat(options.RuntimeOptions.PidFile); err == nil {
		log.Info().Msgf("- remove pid %s", options.RuntimeOptions.PidFile)
		if err = os.Remove(options.RuntimeOptions.PidFile); err != nil {
			log.Error().Err(err).
				Msgf("stop process:%s failed", options.RuntimeOptions.PidFile)
		}
	}

	if _, err := os.Stat(".jvmrc"); err == nil {
		log.Info().Msgf("- Remove .jvmrc %s", options.RuntimeOptions.PidFile)
		if err = os.Remove(".jvmrc"); err != nil {
			log.Error().Err(err).Msg("delete .jvmrc failed")
		}
	}

	util.DropHosts(options.ConnectOptions.Hosts)

	kubernetes, err := cli.Kubernetes()
	if err != nil {
		log.Error().Msgf("fails create kubernetes client when clean up workspace")
		return
	}

	if len(options.RuntimeOptions.Origin) > 0 {
		log.Info().Msgf("- Recover Origin App %s", options.RuntimeOptions.Origin)
		err := kubernetes.ScaleTo(options.RuntimeOptions.Origin, options.Namespace, &options.RuntimeOptions.Replicas)
		if err != nil {
			log.Error().
				Str("namespace", options.Namespace).
				Msgf("scale deployment:%s to %d failed", options.RuntimeOptions.Origin, options.RuntimeOptions.Replicas)
		}
	}

	if len(options.RuntimeOptions.Shadow) > 0 {
		log.Info().Msgf("- clean shadow %s", options.RuntimeOptions.Shadow)
		kubernetes.RemoveDeployment(options.RuntimeOptions.Shadow, options.Namespace)
	}

	if len(options.RuntimeOptions.SSHCM) > 0 {
		log.Info().Msgf("- clean sshcm %s", options.RuntimeOptions.SSHCM)
		kubernetes.RemoveConfigMap(options.RuntimeOptions.SSHCM, options.Namespace)
	}

	removePrivateKey(options)
	if len(options.RuntimeOptions.Service) > 0 {
		log.Info().Msgf("- cleanup service %s", options.RuntimeOptions.Service)
		err := kubernetes.RemoveService(options.RuntimeOptions.Service, options.Namespace)
		if err != nil {
			log.Error().Err(err).Msg("delete service failed")
		}
	}
}

// checkConnectRunning check connect is running and print help msg
func checkConnectRunning(pidFile string) {
	daemonRunning := util.IsDaemonRunning(pidFile)
	if !daemonRunning {
		log.Info().Msgf("'connect' not running, you can only access local app from cluster")
	} else {
		log.Info().Msgf("'connect' is running, you can access local app from cluster and localhost")
	}
}

// removePrivateKey remove the private key of ssh
func removePrivateKey(options *options.DaemonOptions) {
	if options.RuntimeOptions.SSHCM == "" {
		return
	}
	splits := strings.Split(options.RuntimeOptions.SSHCM, "-")
	component, version := splits[1], splits[len(splits)-1]
	file := util.PrivateKeyPath(component, version)
	if err := os.Remove(file); os.IsNotExist(err) {
		log.Error().Err(err).Msgf("can't delete %s", file)
	}
}
