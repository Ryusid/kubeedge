package driver

import (
        "sync"
        "time"

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
        mqttClient     mqtt.Client
        motionStatus   string
        lastUpdate     time.Time
        isConnected    bool
        ProtocolConfig
}

type ProtocolConfig struct {
        ProtocolName string `json:"protocolName"`
        ConfigData   `json:"configData"`
}

type ConfigData struct {
        // MQTT protocol config data for motion detection
        BrokerURL     string `json:"brokerURL"`     // MQTT Broker URL (required)
        ClientID      string `json:"clientID"`      // MQTT Client ID (optional, will auto-generate)
        MotionTopic   string `json:"motionTopic"`   // Topic to subscribe for motion detection (default: "motion")
        Username      string `json:"username"`      // Username for MQTT broker authentication (optional)
        Password      string `json:"password"`      // Password for MQTT broker authentication (optional)
        QoS           int    `json:"qos"`           // QoS level for MQTT (default: 0)
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
