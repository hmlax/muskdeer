package deer

import "github.com/hmlax/muskdeer/deer/rpcclient"

// GRPCClients returns the factory borrowed by consumers. Deer joins its work
// before cleanup; a WithGRPCClients caller then closes its injected factory.
func (d *Deer) GRPCClients() (*rpcclient.Factory, error) {
	if d.rpcClients == nil {
		return nil, ErrGRPCClientsNotConfigured
	}
	return d.rpcClients, nil
}
