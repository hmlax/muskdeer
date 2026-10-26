package frameworkprobe

import (
	"errors"
	"testing"

	"github.com/go-kratos/kratos/contrib/registry/nacos/v2"
	"github.com/go-kratos/kratos/v2/registry"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

const probeGroup = "MUSKDEER_P01"

type namingStub struct {
	naming_client.INamingClient
	registered   vo.RegisterInstanceParam
	deregistered vo.DeregisterInstanceParam
}

func (s *namingStub) RegisterInstance(parameter vo.RegisterInstanceParam) (bool, error) {
	s.registered = parameter
	return true, nil
}

func (s *namingStub) DeregisterInstance(parameter vo.DeregisterInstanceParam) (bool, error) {
	s.deregistered = parameter
	return true, nil
}

func (s *namingStub) SelectInstances(vo.SelectInstancesParam) ([]model.Instance, error) {
	return []model.Instance{
		{
			InstanceId:  "probe-instance",
			Ip:          "127.0.0.1",
			Port:        19090,
			ServiceName: probeServiceName + ".grpc",
			Metadata: map[string]string{
				"kind":    "grpc",
				"version": "probe",
			},
		},
	}, nil
}

func TestNacosRegistrySDKContract(t *testing.T) {
	client := &namingStub{}
	discovery := nacos.New(client, nacos.WithGroup(probeGroup))
	instance := &registry.ServiceInstance{
		ID:        "probe-instance",
		Name:      probeServiceName,
		Version:   "probe",
		Endpoints: []string{"grpc://127.0.0.1:19090"},
	}
	if err := discovery.Register(t.Context(), instance); err != nil {
		t.Fatalf("Nacos Register(probe) = %v", err)
	}
	registered := client.registered
	if registered.ServiceName != probeServiceName+".grpc" || registered.GroupName != probeGroup || registered.Port != 19090 {
		t.Errorf("Nacos SDK registration = %+v; want source service, group and endpoint", registered)
	}
	instances, err := discovery.GetService(t.Context(), probeServiceName+".grpc")
	if err != nil || len(instances) != 1 || len(instances[0].Endpoints) != 1 || instances[0].Endpoints[0] != instance.Endpoints[0] {
		t.Fatalf("Nacos GetService(probe) = %v, %v", instances, err)
	}
	if err := discovery.Deregister(t.Context(), instance); err != nil {
		t.Fatalf("Nacos Deregister(probe) = %v", err)
	}
	if client.deregistered.ServiceName != registered.ServiceName || client.deregistered.GroupName != probeGroup {
		t.Errorf("Nacos deregistration = %+v, want original service and group", client.deregistered)
	}
	if err := discovery.Register(t.Context(), &registry.ServiceInstance{}); !errors.Is(err, nacos.ErrServiceInstanceNameEmpty) {
		t.Errorf("Nacos Register(empty name) = %v, want named validation error", err)
	}
}
