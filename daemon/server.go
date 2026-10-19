package daemon

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/WywySenarios/wc-fuse/policy"
	"github.com/WywySenarios/wc-fuse/protocol"
)

// Config configures the control-socket server.
type Config struct {
	// SocketPath is the unix socket the daemon listens on.
	SocketPath string
	// AllowedUID is the only peer uid accepted (the pod uid). Other
	// host-local users are rejected before any request is parsed.
	AllowedUID uint32
	// Policy is the reloadable host policy.
	Policy *policy.Store
	// Mounts owns the per-project workspace mounts.
	Mounts *MountManager
	// Logger receives diagnostic messages. Optional.
	Logger *log.Logger
}

// Server is the wc-fuse control-socket server.
type Server struct {
	cfg Config
	ln  *net.UnixListener

	mu     sync.Mutex
	closed bool
}

// NewServer binds the control socket. The socket is chmod 0666 because
// no group is shared across the host/pod uid boundary; authorization
// is by peer credentials, not file mode.
func NewServer(cfg Config) (*Server, error) {
	if cfg.SocketPath == "" {
		return nil, errors.New("daemon: socket path is required")
	}
	if cfg.Policy == nil {
		return nil, errors.New("daemon: policy store is required")
	}
	if cfg.Mounts == nil {
		return nil, errors.New("daemon: mount manager is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o711); err != nil {
		return nil, err
	}
	if err := os.Remove(cfg.SocketPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: cfg.SocketPath, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(cfg.SocketPath, 0o666); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return &Server{cfg: cfg, ln: ln}, nil
}

// Addr is the bound socket path.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Serve accepts connections until Close is called.
func (s *Server) Serve() error {
	for {
		conn, err := s.ln.AcceptUnix()
		if err != nil {
			if s.isClosed() {
				return nil
			}
			return err
		}
		go s.handle(conn)
	}
}

// Close stops the listener. It is idempotent.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	return s.ln.Close()
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) logf(format string, args ...any) {
	if s.cfg.Logger != nil {
		s.cfg.Logger.Printf(format, args...)
	}
}

func (s *Server) handle(conn *net.UnixConn) {
	defer conn.Close()

	uid, err := peerUID(conn)
	if err != nil {
		s.logf("peer credentials unavailable: %v", err)
		return
	}
	if uid != s.cfg.AllowedUID {
		s.logf("rejected connection from uid %d (allowed %d)", uid, s.cfg.AllowedUID)
		return
	}

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req protocol.Request
		if err := dec.Decode(&req); err != nil {
			return
		}
		if err := enc.Encode(s.dispatch(&req)); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(req *protocol.Request) protocol.Response {
	switch req.Op {
	case protocol.OpMount:
		if req.ProjectRoot == "" {
			return errResp(protocol.CodeBadRequest, "project_root is required")
		}
		if !s.cfg.Policy.Current().AllowsProjectRoot(req.ProjectRoot) {
			return errResp(protocol.CodeDenied, "project_root is outside the configured project-roots")
		}
		mp, err := s.cfg.Mounts.Mount(req.ProjectRoot, s.cfg.Policy)
		if err != nil {
			return errResp(protocol.CodeInternal, err.Error())
		}
		return protocol.Response{OK: true, MountPoint: mp, OpenMounts: s.cfg.Mounts.OpenMounts()}

	case protocol.OpUnmount:
		if req.ProjectRoot == "" {
			return errResp(protocol.CodeBadRequest, "project_root is required")
		}
		if err := s.cfg.Mounts.Unmount(req.ProjectRoot); err != nil {
			return errResp(protocol.CodeInternal, err.Error())
		}
		return protocol.Response{OK: true, OpenMounts: s.cfg.Mounts.OpenMounts()}

	case protocol.OpStatus:
		return protocol.Response{OK: true, OpenMounts: s.cfg.Mounts.OpenMounts()}

	default:
		return errResp(protocol.CodeBadRequest, "unknown op")
	}
}

func errResp(code, msg string) protocol.Response {
	return protocol.Response{OK: false, Code: code, Error: msg}
}
