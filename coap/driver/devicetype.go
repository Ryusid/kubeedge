package driver

import (
	"context"
	"sync"

	"github.com/kubeedge/mapper-framework/pkg/common"
	udpClient "github.com/plgd-dev/go-coap/v3/udp/client"
)

// CustomizedDev is the customized device configuration and client information.
type CustomizedDev struct {
	Instance         common.DeviceInstance
	CustomizedClient *CustomizedClient
}

// CustomizedClient holds runtime state and protocol config for the device.
type CustomizedClient struct {
	deviceMutex   sync.Mutex
	ProtocolConfig
	motionStatus string
	isConnected  bool

	// CoAP specific fields
	conn   *udpClient.Conn
	cancel context.CancelFunc
}

// ProtocolConfig is the CoAP protocol configuration used by the driver.
type ProtocolConfig struct {
	ProtocolName string `json:"protocolName"`
    ConfigData   `json:"configData"`
	
}
// Adding configdata
type ConfigData struct {
	Addr    string `json:"addr"`    // e.g. "192.168.8.50:5683"
	Path    string `json:"path"`    // e.g. "/motion"
	Observe bool   `json:"observe"` // true to use CoAP Observe
	Timeout string `json:"timeout"` // e.g. "3s"
}

// VisitorConfig holds property visitor configuration.
type VisitorConfig struct {
	ProtocolName      string            `json:"protocolName"`
	VisitorConfigData VisitorConfigData `json:"configData"`
}

// VisitorConfigData describes one visited property.
type VisitorConfigData struct {
	DataType     string `json:"dataType"`
	PropertyName string `json:"propertyName"`
}
