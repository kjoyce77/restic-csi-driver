package server

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"nodeto/restic-csi-plugin/config"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	// DefaultDriverName defines the name that is used in Kubernetes and the CSI
	// system for the canonical, official name of this plugin
	DefaultDriverName = "restic.csi.nodeto.com"
)

var (
	gitTreeState = "not a git tree"
	commit       string
	version      string
)

// Driver implements the following CSI interfaces:
//
//	csi.IdentityServer
//	csi.NodeServer
type Driver struct {
	name string
	// publishInfoVolumeName is used to pass the volume name from
	// `ControllerPublishVolume` to `NodeStageVolume or `NodePublishVolume`
	publishInfoVolumeName string

	endpoint string
	hostID   string

	srv *grpc.Server
	log *logrus.Entry
	config *config.Config

	// ready defines whether the driver is ready to function. This value will
	// be used by the `Identity` service via the `Probe()` method.
	readyMu sync.Mutex // protects ready
	ready   bool
}

func GetVersion() string {
	return version
}

func GetCommit() string {
	return commit
}

func GetTreeState() string {
	return gitTreeState
}

func NewDriver(ep string, driverName string, nodeId string, cfg *config.Config) (*Driver, error) {
	if driverName == "" {
		driverName = DefaultDriverName
	}

	if version == "" {
		version = "dev"
	}

	log := logrus.New().WithFields(logrus.Fields{
		"version": version,
	})

	return &Driver{
		name:                  driverName,
		publishInfoVolumeName: driverName + "/volume-name",
		hostID:                nodeId,

		endpoint: ep,
		log:      log,
		config:   cfg,
	}, nil
}

// Run starts the CSI plugin by communication over the given endpoint
func (d *Driver) Run(ctx context.Context) error {
	u, err := url.Parse(d.endpoint)
	if err != nil {
		return fmt.Errorf("unable to parse address: %q", err)
	}

	grpcAddr := path.Join(u.Host, filepath.FromSlash(u.Path))
	if u.Host == "" {
		grpcAddr = filepath.FromSlash(u.Path)
	}

	// CSI plugins talk only over UNIX sockets currently
	if u.Scheme != "unix" {
		return fmt.Errorf("currently only unix domain sockets are supported, have: %s", u.Scheme)
	}
	// remove the socket if it's already there. This can happen if we
	// deploy a new version and the socket was created from the old running
	// plugin.
	d.log.WithField("socket", grpcAddr).Info("removing socket")
	if err := os.Remove(grpcAddr); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unix domain socket file %s, error: %s", grpcAddr, err)
	}

	grpcListener, err := net.Listen(u.Scheme, grpcAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	// log response errors for better observability
	errHandler := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			d.log.WithError(err).WithField("method", info.FullMethod).Error("method failed")
		}
		return resp, err
	}

	d.srv = grpc.NewServer(grpc.UnaryInterceptor(errHandler))
	reflection.Register(d.srv)
	csi.RegisterIdentityServer(d.srv, d)
	csi.RegisterNodeServer(d.srv, d)

	d.ready = true // we're now ready to go!
	d.log.WithFields(logrus.Fields{
		"grpc_addr": grpcAddr,
	}).Info("starting server")

	var eg errgroup.Group
	eg.Go(func() error {
		go func() {
			<-ctx.Done()
			d.log.Info("server stopped")
			d.readyMu.Lock()
			d.ready = false
			d.readyMu.Unlock()
			d.srv.GracefulStop()
		}()
		return d.srv.Serve(grpcListener)
	})

	return eg.Wait()
}
