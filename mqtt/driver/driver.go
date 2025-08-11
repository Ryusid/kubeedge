package driver

import (
        "fmt"
        "sync"
        "time"
        mqtt "github.com/eclipse/paho.mqtt.golang"
        "k8s.io/klog/v2"
        "github.com/kubeedge/mapper-framework/pkg/common"
)

func NewClient(protocol ProtocolConfig) (*CustomizedClient, error) {
        client := &CustomizedClient{
                ProtocolConfig: protocol,
                deviceMutex:    sync.Mutex{},
                motionStatus:   "no_motion",
                isConnected:    false,
        }
        return client, nil
}

func (c *CustomizedClient) InitDevice() error {
    // Preserve current state across re-inits
    previousState := c.motionStatus
    klog.Infof("Initializing motion detection device with broker: %s (preserving state: %s)",
        c.ProtocolConfig.BrokerURL, previousState)

    // Validate required configuration
    if c.ProtocolConfig.BrokerURL == "" {
        return fmt.Errorf("brokerURL is required in protocol config")
    }

    // Defaults
    if c.ProtocolConfig.ClientID == "" {
        c.ProtocolConfig.ClientID = fmt.Sprintf("motion-mapper-%d", time.Now().Unix())
    }
    if c.ProtocolConfig.MotionTopic == "" {
        c.ProtocolConfig.MotionTopic = "motion"
    }

    // MQTT client options
    opts := mqtt.NewClientOptions()
    opts.AddBroker(c.ProtocolConfig.BrokerURL)
    opts.SetClientID(c.ProtocolConfig.ClientID)
    opts.SetCleanSession(true)
    opts.SetAutoReconnect(true)
    opts.SetKeepAlive(30 * time.Second)
    opts.SetPingTimeout(10 * time.Second)
    opts.SetConnectTimeout(30 * time.Second)
    opts.SetMaxReconnectInterval(5 * time.Second)

    if c.ProtocolConfig.Username != "" {
        opts.SetUsername(c.ProtocolConfig.Username)
    }
    if c.ProtocolConfig.Password != "" {
        opts.SetPassword(c.ProtocolConfig.Password)
    }

    // Handlers
    opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
        klog.Errorf("MQTT connection lost: %v", err)
        c.deviceMutex.Lock()
        c.isConnected = false
        c.deviceMutex.Unlock()
    })

    opts.SetOnConnectHandler(func(client mqtt.Client) {
        klog.Infof("MQTT connected successfully")
        c.deviceMutex.Lock()
        c.isConnected = true
        c.deviceMutex.Unlock()

        qos := byte(c.ProtocolConfig.QoS)
        if token := client.Subscribe(c.ProtocolConfig.MotionTopic, qos, c.onMotionMessage); token.Wait() && token.Error() != nil {
            klog.Errorf("Failed to subscribe to motion topic: %v", token.Error())
        } else {
            klog.Infof("Successfully subscribed to motion topic: %s", c.ProtocolConfig.MotionTopic)
        }
    })

    // Connect
    c.mqttClient = mqtt.NewClient(opts)
    if token := c.mqttClient.Connect(); token.Wait() && token.Error() != nil {
        return fmt.Errorf("failed to connect to MQTT broker: %v", token.Error())
    }

    // Restore previous state (avoid resetting to default on re-init)
    if previousState != "" {
        c.motionStatus = previousState
        klog.Infof("Restored motion status to: %s after reconnection", c.motionStatus)
    } else if c.motionStatus == "" {
        c.motionStatus = "no_motion"
        klog.Infof("Initial motion status set to: %s", c.motionStatus)
    }

    klog.Infof("Motion detection device initialized successfully with status: %s", c.motionStatus)
    return nil
}

func (c *CustomizedClient) GetDeviceData(visitor *VisitorConfig) (interface{}, error) {
        c.deviceMutex.Lock()
        defer c.deviceMutex.Unlock()
        
        klog.V(2).Infof("GetDeviceData called for property: %s", visitor.VisitorConfigData.PropertyName)
        
        switch visitor.VisitorConfigData.PropertyName {
        case "motion":
                klog.V(2).Infof("Returning motion status: %s", c.motionStatus)
                return c.motionStatus, nil
        default:
                return nil, fmt.Errorf("unknown property: %s", visitor.VisitorConfigData.PropertyName)
        }
}

func (c *CustomizedClient) DeviceDataWrite(visitor *VisitorConfig, deviceMethodName string, propertyName string, data interface{}) error {
        // Motion detection is typically read-only, but we can implement this for completeness
        klog.V(3).Infof("DeviceDataWrite called for property: %s with data: %v", propertyName, data)
        return nil
}

func (c *CustomizedClient) SetDeviceData(data interface{}, visitor *VisitorConfig) error {
        // Motion detection is typically read-only from the device perspective
        klog.V(3).Infof("SetDeviceData called with data: %v", data)
        return nil
}

func (c *CustomizedClient) StopDevice() error {
        klog.Infof("Stopping motion detection device")
        
        c.deviceMutex.Lock()
        defer c.deviceMutex.Unlock()
        
        if c.mqttClient != nil && c.mqttClient.IsConnected() {
                // Unsubscribe from motion topic
                if token := c.mqttClient.Unsubscribe(c.ConfigData.MotionTopic); token.Wait() && token.Error() != nil {
                        klog.Errorf("Failed to unsubscribe from motion topic: %v", token.Error())
                }
                
                // Disconnect MQTT client
                c.mqttClient.Disconnect(250)
                klog.Infof("MQTT client disconnected")
        }
        
        c.isConnected = false
        return nil
}

func (c *CustomizedClient) GetDeviceStates() (string, error) {
        c.deviceMutex.Lock()
        defer c.deviceMutex.Unlock()
        
        if c.isConnected && c.mqttClient != nil && c.mqttClient.IsConnected() {
                return common.DeviceStatusOK, nil
        }
        return common.DeviceStatusDisCONN, nil
}

// MQTT message callback for motion detection
func (c *CustomizedClient) onMotionMessage(client mqtt.Client, msg mqtt.Message) {
        klog.V(2).Infof("Motion message received on topic %s: %s", msg.Topic(), string(msg.Payload()))
        
        c.deviceMutex.Lock()
        defer c.deviceMutex.Unlock()
        
        // Update motion status based on message content
        oldStatus := c.motionStatus
        c.motionStatus = string(msg.Payload())
        
        if oldStatus != c.motionStatus {
                klog.Infof("Motion status changed from '%s' to '%s' - twin will be updated on next collection cycle", oldStatus, c.motionStatus)
        } else {
                klog.V(2).Infof("Motion status unchanged: '%s'", c.motionStatus)
        }
}