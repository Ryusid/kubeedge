package driver

import (
    "context"
    "encoding/json"
    "fmt"
    "strconv"
    "sync"
    "time"

    "github.com/plgd-dev/go-coap/v3/message/codes"
    "github.com/plgd-dev/go-coap/v3/udp/client"
    "github.com/plgd-dev/go-coap/v3/udp/message/pool"
    "k8s.io/klog/v2"

    "github.com/kubeedge/kubeedge/pkg/apis/devices/v1beta1"
    "github.com/kubeedge/mappers-go/mappers/common"
)

// ProtocolConfig is CoAP specific configuration
type ProtocolConfig struct {
    Addr    string `json:"addr"`
    Path    string `json:"path"`
    Observe bool   `json:"observe"`
    Timeout string `json:"timeout"`
}

// VisitorConfig handles visitor configuration for properties
type VisitorConfig struct {
    ProtocolName       string            `json:"protocolName"`
    VisitorConfigData  VisitorConfigData `json:"configData"`
}

type VisitorConfigData struct {
    PropertyName string `json:"propertyName"`
    DataType     string `json:"dataType"`
}

// CustomizedClient implements the CoAP client
type CustomizedClient struct {
    ProtocolConfig ProtocolConfig
    deviceMutex    sync.Mutex
    motionStatus   string
    isConnected    bool

    conn   *client.ClientConn
    cancel context.CancelFunc
}

// CustomizedDev represents a customized device
type CustomizedDev struct {
    Instance         common.DeviceInstance
    CustomizedClient *CustomizedClient
}

// NewClient creates a new CoAP client
func NewClient(protocolConfig ProtocolConfig) (*CustomizedClient, error) {
    client := &CustomizedClient{
        ProtocolConfig: protocolConfig,
        deviceMutex:    sync.Mutex{},
        motionStatus:   "no_motion",
        isConnected:    false,
    }
    return client, nil
}

func (c *CustomizedClient) InitDevice() error {
    // Preserve current state across re-inits
    previousState := c.motionStatus
    klog.Infof("Initializing CoAP device with addr: %s (preserving state: %s)",
        c.ProtocolConfig.Addr, previousState)

    // Validate required configuration
    if c.ProtocolConfig.Addr == "" {
        return fmt.Errorf("addr is required in protocol config")
    }

    // Set defaults
    if c.ProtocolConfig.Path == "" {
        c.ProtocolConfig.Path = "/motion"
    }

    // Parse timeout
    timeout := 3 * time.Second
    if c.ProtocolConfig.Timeout != "" {
        if parsed, err := time.ParseDuration(c.ProtocolConfig.Timeout); err == nil {
            timeout = parsed
        }
    }

    ctx, cancel := context.WithCancel(context.Background())
    c.cancel = cancel

    conn, err := client.Dial("udp", c.ProtocolConfig.Addr)
    if err != nil {
        return fmt.Errorf("failed to connect to CoAP server: %v", err)
    }
    c.conn = conn

    c.deviceMutex.Lock()
    c.isConnected = true
    c.deviceMutex.Unlock()

    klog.Infof("CoAP connected successfully to %s", c.ProtocolConfig.Addr)

    if c.ProtocolConfig.Observe {
        _, err := conn.Observe(ctx, c.ProtocolConfig.Path, func(n *pool.Message) {
            payload, _ := n.ReadBody()
            val := string(payload)
            if val == "" {
                val = "no_motion"
            }
            c.deviceMutex.Lock()
            old := c.motionStatus
            c.motionStatus = val
            c.deviceMutex.Unlock()
            if old != val {
                klog.Infof("CoAP observe notification: motion status changed from '%s' to '%s'", old, val)
            }
        })
        if err != nil {
            return fmt.Errorf("failed to observe %s: %v", c.ProtocolConfig.Path, err)
        }
        klog.Infof("Successfully observing CoAP resource: %s", c.ProtocolConfig.Path)
    }

    // Restore previous state if it was meaningful
    if previousState != "" {
        c.motionStatus = previousState
        klog.Infof("Restored motion status to: %s after reconnection", c.motionStatus)
    }

    klog.Infof("CoAP device initialized successfully with status: %s", c.motionStatus)
    return nil
}

func (c *CustomizedClient) StopDevice() error {
    klog.Infof("Stopping CoAP device")

    c.deviceMutex.Lock()
    c.isConnected = false
    c.deviceMutex.Unlock()

    if c.cancel != nil {
        c.cancel()
    }
    if c.conn != nil {
        _ = c.conn.Close()
    }

    klog.Infof("CoAP client disconnected")
    return nil
}

// GetDeviceData returns device data for a specific property
func (c *CustomizedClient) GetDeviceData(visitor *VisitorConfig) (interface{}, error) {
    c.deviceMutex.Lock()
    defer c.deviceMutex.Unlock()

    klog.V(2).Infof("GetDeviceData called for property: %s", visitor.VisitorConfigData.PropertyName)

    switch visitor.VisitorConfigData.PropertyName {
    case "motion":
        if c.ProtocolConfig.Observe {
            // In observe mode, return cached status
            klog.V(2).Infof("Returning cached motion status: %s", c.motionStatus)
            return c.motionStatus, nil
        }

        // Polling mode - make GET request
        if !c.isConnected || c.conn == nil {
            return "no_motion", fmt.Errorf("CoAP client not connected")
        }

        ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
        defer cancel()

        resp, err := c.conn.Get(ctx, c.ProtocolConfig.Path)
        if err != nil {
            klog.Warningf("CoAP GET failed: %v, returning cached status", err)
            return c.motionStatus, nil
        }

        if resp.Code() != codes.Content {
            klog.Warningf("CoAP GET returned %v, returning cached status", resp.Code())
            return c.motionStatus, nil
        }

        body, _ := resp.ReadBody()
        val := string(body)
        if val == "" {
            val = "no_motion"
        }

        c.motionStatus = val
        klog.V(2).Infof("CoAP GET returned motion status: %s", c.motionStatus)
        return c.motionStatus, nil

    default:
        return nil, fmt.Errorf("unknown property: %s", visitor.VisitorConfigData.PropertyName)
    }
}

// SetDeviceData sets device data (not implemented for motion sensor)
func (c *CustomizedClient) SetDeviceData(data interface{}) error {
    return fmt.Errorf("set operation not supported for motion sensor")
}

// GetTwinData gets twin data
func (c *CustomizedClient) GetTwinData(deviceID string, client *common.EdgeCoreClient) error {
    return nil
}

// ParseProtocolFromGrpc parses protocol configuration from gRPC
func ParseProtocolFromGrpc(protocol *v1beta1.ProtocolConfig) (ProtocolConfig, error) {
    protocolConfigData := ProtocolConfig{}
    if protocol.ConfigData != nil {
        if err := json.Unmarshal(protocol.ConfigData, &protocolConfigData); err != nil {
            return protocolConfigData, err
        }
    }
    return protocolConfigData, nil
}

// ParseVisitorConfigFromGrpc parses visitor configuration from gRPC
func ParseVisitorConfigFromGrpc(visitor *v1beta1.DevicePropertyVisitor) (VisitorConfig, error) {
    visitorConfig := VisitorConfig{}
    visitorConfig.ProtocolName = visitor.ProtocolName
    if visitor.ConfigData != nil {
        if err := json.Unmarshal(visitor.ConfigData, &visitorConfig.VisitorConfigData); err != nil {
            return visitorConfig, err
        }
    }
    return visitorConfig, nil
}