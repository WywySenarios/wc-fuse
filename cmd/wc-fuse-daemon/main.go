// Command wc-fuse-daemon is the host-side wc-fuse daemon.
//
// It reads the host policy (fs.yaml + project-roots.yaml) from the
// config clone under /etc/wc/config.d, listens on the control socket,
// and serves per-project workspace mounts for the pod.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WywySenarios/wc-fuse/daemon"
	"github.com/WywySenarios/wc-fuse/fusefs"
	"github.com/WywySenarios/wc-fuse/policy"
)

func main() {
	var (
		socket      = flag.String("socket", "/dev/wc/control/wc-fuse.sock", "control socket path")
		workspace   = flag.String("workspace", "/dev/wc/workspace", "workspace mount tree")
		configDir   = flag.String("config", "/etc/wc/config.d", "host config clone")
		allowedUID  = flag.Uint("allowed-uid", 10001, "pod uid allowed to connect")
		reloadEvery = flag.Duration("reload-interval", 2*time.Second, "policy reload poll interval (0 disables)")
	)
	flag.Parse()

	pol, err := policy.Load(*configDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wc-fuse-daemon:", err)
		os.Exit(1)
	}
	store := policy.NewStore(pol)

	mounts := daemon.NewMountManager(fuseMounter{}, *workspace)
	srv, err := daemon.NewServer(daemon.Config{
		SocketPath: *socket,
		AllowedUID: uint32(*allowedUID),
		Policy:     store,
		Mounts:     mounts,
		Logger:     log.New(os.Stderr, "wc-fuse-daemon: ", log.LstdFlags),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "wc-fuse-daemon:", err)
		os.Exit(1)
	}

	if *reloadEvery > 0 {
		stop := make(chan struct{})
		defer close(stop)
		go watchPolicy(*configDir, store, *reloadEvery, stop)
	}

	go handleSignals(srv)

	log.Printf("listening on %s (workspace %s, allowed uid %d, %d project root(s))",
		srv.Addr(), *workspace, *allowedUID, len(pol.ProjectRoots()))
	if err := srv.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, "wc-fuse-daemon:", err)
		os.Exit(1)
	}
}

// fuseMounter adapts fusefs.Mount to daemon.Mounter.
type fuseMounter struct{}

func (fuseMounter) Mount(target, mountpoint string, store *policy.Store) (daemon.MountServer, error) {
	srv, err := fusefs.Mount(target, mountpoint, store)
	if err != nil {
		return nil, err
	}
	return srv, nil
}

// watchPolicy reloads the policy when fs.yaml or project-roots.yaml
// changes, so a host-side config change takes effect without a restart
// and without touching the pod.
func watchPolicy(dir string, store *policy.Store, every time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	last := latestModTime(dir)
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			mt := latestModTime(dir)
			if !mt.After(last) {
				continue
			}
			pol, err := policy.Load(dir)
			if err != nil {
				log.Printf("policy reload failed: %v", err)
				continue
			}
			store.Replace(pol)
			last = mt
			log.Printf("policy reloaded from %s", dir)
		}
	}
}

func latestModTime(dir string) time.Time {
	var latest time.Time
	for _, name := range []string{"fs.yaml", "project-roots.yaml"} {
		info, err := os.Stat(dir + "/" + name)
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

func handleSignals(srv *daemon.Server) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Printf("shutting down")
	_ = srv.Close()
}
