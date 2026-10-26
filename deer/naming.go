package deer

import (
	"context"
	"net"
	"strconv"

	"github.com/hmlax/muskdeer/deer/nacos"
)

const grpcServiceSuffix = ".grpc"

func (d *Deer) configurePublication() error {
	registration := d.cfg.Nacos.GetRegistration()
	if registration == nil {
		return nil
	}
	_, rawPort, err := net.SplitHostPort(d.grpcAddress)
	if err != nil {
		return err
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		return err
	}
	d.publication, err = nacos.NewPublication(d.discovery, nacos.Registration{
		ID:      d.cfg.InstanceId,
		Service: d.cfg.Name + grpcServiceSuffix,
		Version: d.cfg.Version,
		IP:      registration.AdvertiseIp,
		Port:    uint32(port),
	}, registration.HeartbeatInterval.AsDuration())
	return err
}

func (d *Deer) stopPublication(ctx context.Context) error {
	if d.publication == nil {
		return nil
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.cfg.ShutdownTimeout.AsDuration())
	defer cancel()
	return d.publication.Stop(shutdown)
}
