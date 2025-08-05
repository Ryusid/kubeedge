package driver

import (
        "fmt"
        "strconv"
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
                lastUpdate:     time.Now(),
                isConnected:    false,
        }
        return client, nil
}

func (c *CustomizedClient) InitDevice() error {
        klog.Infof("Initializing motion detection device with broker: %s", c.ConfigData.BrokerURL)
        
        // Validate required configuration
        if c.ConfigData.BrokerURL == "" {
                return fmt.Errorf("brokerURL is required in protocol config")
        }
        
        // Set defaults
        if c.ConfigData.ClientID == "" {
                c.ConfigData.ClientID = fmt.Sprintf("motion-mapper-%d", time.Now().Unix())
        }
        if c.ConfigData.MotionTopic == "" {
                c.ConfigData.MotionTopic = "motion"
        }
        if c.ConfigData.QoS == 0 {
                c.ConfigData.QoS = 0 // Default QoS
        }
        
        // Create MQTT client options
        opts := mqtt.NewClientOptions()
        opts.AddBroker(c.ConfigData.BrokerURL)
        opts.SetClientID(c.ConfigData.ClientID)
        opts.SetCleanSession(true)
        opts.SetAutoReconnect(true)
        
        if c.ConfigData.Username != "" {
                opts.SetUsername(c.ConfigData.Username)
        }
        if c.ConfigData.Password != "" {
                opts.SetPassword(c.ConfigData.Password)
        }
        
        // Set connection lost handler
        opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
                klog.Errorf("MQTT connection lost: %v", err)
                c.deviceMutex.Lock()
                c.isConnected = false
                c.deviceMutex.Unlock()
        })
        
        // Set on connect handler
        opts.SetOnConnectHandler(func(client mqtt.Client) {
                klog.Infof("MQTT connected successfully")
                c.deviceMutex.Lock()
                c.isConnected = true
                c.deviceMutex.Unlock()
                
                // Subscribe to motion topic
                if token := client.Subscribe(c.ConfigData.MotionTopic, byte(c.ConfigData.QoS), c.onMotionMessage); token.Wait() && token.Error() != nil {
                        klog.Errorf("Failed to subscribe to motion topic: %v", token.Error())
                } else {
                        klog.Infof("Successfully subscribed to motion topic: %s", c.ConfigData.MotionTopic)
                }
        })
        
        // Create and connect MQTT client
        c.mqttClient = mqtt.NewClient(opts)
        if token := c.mqttClient.Connect(); token.Wait() && token.Error() != nil {
                return fmt.Errorf("failed to connect to MQTT broker: %v", token.Error())
        }
        
        klog.Infof("Motion detection device initialized successfully")
        return nil
}

func (c *CustomizedClient) GetDeviceData(visitor *VisitorConfig) (interface{}, error) {
        c.deviceMutex.Lock()
        defer c.deviceMutex.Unlock()
        
        switch visitor.VisitorConfigData.PropertyName {
        case "motion":
                return c.motionStatus, nil
        case "timestamp":
                return strconv.FormatInt(c.lastUpdate.Unix(), 10), nil
        case "status":
                if c.isConnected {
                        return "online", nil
                }
                return "offline", nil
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
        c.motionStatus = string(msg.Payload())
        c.lastUpdate = time.Now()
        
        klog.V(1).Infof("Motion status updated: %s at %v", c.motionStatus, c.lastUpdate)
}
