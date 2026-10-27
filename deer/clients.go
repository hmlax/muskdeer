package deer

import "github.com/hmlax/muskdeer/deer/rpcclient"

// GRPCClients returns the borrowed factory. Callers must not close it; Deer
// cancels/joins calls before releasing its discovery and other shared resources.
func (d *Deer) GRPCClients() (*rpcclient.Factory, error) {
	if d.rpcClients == nil {
		return nil, ErrGRPCClientsNotConfigured
	}
	return d.rpcClients, nil
}
