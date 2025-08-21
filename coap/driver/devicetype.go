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
	motion bool
	lastDetected string
	class string
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
	// resource paths
	MotionPath    string `json:"motionPath"`    // "/motion"
	LastPath      string `json:"lastPath"`      // "/last_detection"
	ClassPath     string `json:"classPath"`     // "/class"

	ObserveMotion bool   `json:"observeMotion"` // true to use CoAP Observe on motion
	ObserveLast bool   `json:"observeLast"` // true to use CoAP Observe on last_detection
	ObserveClass bool   `json:"observeClass"` // true to use CoAP Observe on class
	Timeout string `json:"timeout"` // e.g. "5s"
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
