package driver

import (
        "sync"

        mqtt "github.com/eclipse/paho.mqtt.golang"
        "github.com/kubeedge/mapper-framework/pkg/common"
)

// CustomizedDev is the customized device configuration and client information.
type CustomizedDev struct {
        Instance         common.DeviceInstance
        CustomizedClient *CustomizedClient
}

type CustomizedClient struct {
        // Motion detection specific variables
        deviceMutex    sync.Mutex
        motionStatus   string
        isConnected    bool
        ProtocolConfig
        // CoAP specific fields
        conn   *client.ClientConn
        cancel context.CancelFunc
}

type ProtocolConfig struct {
        ProtocolName string `json:"protocolName"`
        ConfigData   `json:"configData"`
}

type ConfigData struct {
        // CoAP protocol config data for motion detection
        Addr    string `json:"addr"`    // CoAP server address (required) e.g., "192.168.8.50:5683"
        Path    string `json:"path"`    // Resource path (default: "/motion")
        Observe bool   `json:"observe"` // Use CoAP Observe for push notifications (default: false)
        Timeout string `json:"timeout"` // Request timeout (default: "3s")
}

type VisitorConfig struct {
        ProtocolName      string `json:"protocolName"`
        VisitorConfigData `json:"configData"`
}

type VisitorConfigData struct {
        // Visitor config for accessing device properties
        DataType     string `json:"dataType"`     // Data type of the property (string, int, etc.)
        PropertyName string `json:"propertyName"` // Name of the property to access (motion, timestamp, status)
}
