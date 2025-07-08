package kcpenvtest

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/platform-mesh/golang-commons/logger"

	"github.com/platform-mesh/account-operator/pkg/testing/kcpenvtest/process"
)

type KCPServer struct {
	processState *process.State
	Out          io.Writer
	Err          io.Writer
	StartTimeout time.Duration
	StopTimeout  time.Duration
	Dir          string
	Binary       string
	Args         []string
	PathToRoot   string

	log  *logger.Logger
	args *process.Arguments
}

func NewKCPServer(baseDir string, binary string, pathToRoot string, log *logger.Logger) *KCPServer {
	return &KCPServer{
		Dir:        baseDir,
		Binary:     binary,
		Args:       []string{"start", "-v=1"},
		PathToRoot: pathToRoot,
		log:        log,
	}
}

func (s *KCPServer) Start() error {
	if err := s.prepare(); err != nil {
		return err
	}
	return s.processState.Start(s.Out, s.Err, s.log)
}

func (s *KCPServer) prepare() error {
	if s.Out == nil || s.Err == nil {
		//create file writer for the logs
		fileOut := filepath.Join(s.PathToRoot, "kcp.log")
		out, err := os.Create(fileOut)
		if err != nil {
			return err
		}
		writer := io.Writer(out)

		if s.Out == nil {
			s.Out = writer
		}
		if s.Err == nil {
			s.Err = writer
		}
	}

	if err := s.setProcessState(); err != nil {
		return err
	}
	return nil
}

func (s *KCPServer) setProcessState() error {
	var err error

	healthUrl, err := url.Parse("https://localhost:6443/clusters/root/apis/tenancy.kcp.io/v1alpha1/workspaces")
	if err != nil {
		return err
	}
	s.processState = &process.State{
		Dir:          s.Dir,
		Path:         s.Binary,
		StartTimeout: s.StartTimeout,
		StopTimeout:  s.StopTimeout,
		HealthCheck: process.HealthCheck{
			URL:          *healthUrl,
			PollInterval: 2 * time.Second,
			KcpAssetPath: filepath.Join(s.PathToRoot, ".kcp"),
		},
	}
	if err := s.processState.Init("kcp"); err != nil {
		return err
	}

	s.Binary = s.processState.Path
	s.Dir = s.processState.Dir
	s.StartTimeout = s.processState.StartTimeout
	s.StopTimeout = s.processState.StopTimeout

	s.processState.Args, s.Args, err = process.TemplateAndArguments(s.Args, s.Configure(), process.TemplateDefaults{ //nolint:staticcheck
		Data:            s,
		Defaults:        s.defaultArgs(),
		MinimalDefaults: map[string][]string{},
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *KCPServer) defaultArgs() map[string][]string {
	args := map[string][]string{}
	return args
}

func (s *KCPServer) Configure() *process.Arguments {
	if s.args == nil {
		s.args = process.EmptyArguments()
	}
	return s.args
}

func (s *KCPServer) Stop() error {
	if s.processState != nil {
		if err := s.processState.Stop(); err != nil {
			return err
		}
	}
	return nil
}
