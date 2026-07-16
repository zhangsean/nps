package proxy

import (
	"net"
	"net/http"
	"strconv"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/fileserver"
	"github.com/astaxie/beego/logs"
)

type LocalFileServer struct {
	BaseServer
	listener net.Listener
	server   *http.Server
}

func NewLocalFileServer(task *file.Tunnel) *LocalFileServer {
	s := new(LocalFileServer)
	s.task = task
	return s
}

func (s *LocalFileServer) Start() error {
	if s.task.ServerIp == "" {
		s.task.ServerIp = "0.0.0.0"
	}
	s.task.LocalPath = fileserver.NormalizeRoot(s.task.LocalPath)
	if err := fileserver.EnsureRoot(s.task.LocalPath); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", s.task.ServerIp+":"+strconv.Itoa(s.task.Port))
	if err != nil {
		return err
	}
	s.listener = listener
	s.server = &http.Server{
		Handler: fileserver.NewBrowser(s.task.LocalPath, s.task.StripPre),
	}
	logs.Info("local file server start, local path %s, strip prefix %s, port %d", s.task.LocalPath, s.task.StripPre, s.task.Port)
	return s.server.Serve(listener)
}

func (s *LocalFileServer) Close() error {
	if s.server != nil {
		return s.server.Close()
	}
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
