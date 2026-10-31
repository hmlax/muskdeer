package adminhttp

import (
	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/internal/serviceoverview"
)

func (r *Routes) assembleServices(cfg *configv1.ServiceConfig, owner *deer.Deer, clients serviceoverview.Clients) error {
	discovery, err := owner.Discovery()
	if err != nil {
		return err
	}
	r.services, err = serviceoverview.New(r.auth, discovery, clients, serviceoverview.LocalAPI{
		InstanceID: cfg.InstanceId,
		Version:    cfg.Version,
		CheckReady: owner.CheckReady,
	}, serviceoverview.Settings{AllowedServices: cfg.GrpcClients.AllowedServices})
	return err
}
