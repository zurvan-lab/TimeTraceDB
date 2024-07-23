package server

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/zurvan-lab/timetrace/config"
	"github.com/zurvan-lab/timetrace/core/TQL/execute"
	parser "github.com/zurvan-lab/timetrace/core/TQL/parser"
	"github.com/zurvan-lab/timetrace/core/database"
	ttlog "github.com/zurvan-lab/timetrace/log"
)

type Server struct {
	ListenAddress     string
	Listener          net.Listener
	QuitChan          chan struct{}
	Wg                sync.WaitGroup
	ActiveConnections map[net.Conn]*config.User
	ActiveConnsMux    sync.Mutex
	Config            *config.Config

	db *database.Database
}

func NewServer(cfg *config.Config, db *database.Database) *Server {
	lna := fmt.Sprintf("%v:%v", cfg.Server.IP, cfg.Server.Port)

	return &Server{
		ListenAddress:     lna,
		QuitChan:          make(chan struct{}),
		ActiveConnections: make(map[net.Conn]*config.User),
		db:                db,
		Config:            cfg,
	}
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.ListenAddress)
	if err != nil {
		return err
	}

	s.Listener = listener

	ttlog.Info("server started", "address", s.ListenAddress, "db-name", s.Config.Name)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalChan
		ttlog.Info("Received signal, shutting down...", "signal", sig, "db-name", s.Config.Name)

		close(s.QuitChan)
		s.Stop()
	}()

	s.Wg.Add(1)
	go s.AcceptConnections()

	<-s.QuitChan

	return nil
}

func (s *Server) AcceptConnections() {
	defer s.Wg.Done()

	for {
		select {
		case <-s.QuitChan:
			return
		default:
		}

		conn, err := s.Listener.Accept()
		if err != nil {
			ttlog.Error("error accepting connection", "error", err, "db-name", s.Config.Name)

			continue
		}

		user, err := s.Authenticate(conn)
		if err != nil {
			ttlog.Warn("invalid user try to connect", "db-name", s.Config.Name)
		} else {
			s.ActiveConnsMux.Lock()
			s.ActiveConnections[conn] = user
			s.ActiveConnsMux.Unlock()

			ttlog.Info("new connection", "remote address", conn.RemoteAddr().String(), "db-name", s.Config.Name)

			s.Wg.Add(1)
			go s.HandleConnection(conn)
		}
	}
}

func (s *Server) HandleConnection(conn net.Conn) {
	defer conn.Close()
	defer s.Wg.Done()

	buffer := make([]byte, 2050)

	for {
		user := s.ActiveConnections[conn]

		n, err := conn.Read(buffer)
		if err != nil {
			ttlog.Info("Connection closed", "remote address", conn.RemoteAddr().String(), "db-name", s.Config.Name)

			s.ActiveConnsMux.Lock()
			delete(s.ActiveConnections, conn)
			s.ActiveConnsMux.Unlock()

			return
		}

		query := parser.ParseQuery(string(buffer[:n]))

		if user.HasAccess(query.Command) {
			result := execute.Execute(query, s.db)

			_, err = conn.Write([]byte(result))
			if err != nil {
				ttlog.Error("Can't write on TCP connection", "error", err, "db-name", s.Config.Name)
			}
		} else {
			_, _ = conn.Write([]byte(database.INVALID))
		}
	}
}

func (s *Server) Authenticate(conn net.Conn) (*config.User, error) {
	buffer := make([]byte, 1024)

	n, err := conn.Read(buffer)
	if err != nil {
		ttlog.Error("error reading connection", "error", err, "db-name", s.Config.Name)

		_ = conn.Close()
	}

	query := parser.ParseQuery(string(buffer[:n]))
	if query.Command != "CON" {
		_ = conn.Close()

		return nil, AuthenticateError{
			reason: "user need to make a session first",
		}
	}

	result := execute.Execute(query, s.db)
	if result != database.OK {
		_ = conn.Close()

		return nil, AuthenticateError{
			reason: "user name or password is invalid",
		}
	}

	_, _ = conn.Write([]byte(result))

	var user *config.User

	for _, u := range s.Config.Users {
		if u.Name == query.Args[0] {
			user = &u
		}
	}

	return user, nil
}

func (s *Server) Stop() {
	s.ActiveConnsMux.Lock()
	defer s.ActiveConnsMux.Unlock()

	for conn := range s.ActiveConnections {
		conn.Close()
		delete(s.ActiveConnections, conn)
	}

	s.Listener.Close()
}
