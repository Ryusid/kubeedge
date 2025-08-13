package driver

import (
	"encoding/json"
	"context"
	"fmt"
	"github.com/kubeedge/api/apis/devices/v1beta1"
	"time"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/message/pool"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/kubeedge/mapper-framework/pkg/common"
	"k8s.io/klog/v2"
)

func NewClient(protocolConfig ProtocolConfig) (*CustomizedClient, error) {
	c := &CustomizedClient{
		ProtocolConfig: protocolConfig,
		motionStatus:   "no_motion",
		isConnected:    false,
	}
	return c, nil
}

func (c *CustomizedClient) InitDevice() error {
	prev := c.motionStatus
	klog.Infof("Initializing CoAP device with addr: %s (preserving state: %s)", c.Addr, prev)

	if c.ProtocolConfig.Addr == "" {
		return fmt.Errorf("addr is required in protocol config")
	}
	if c.ProtocolConfig.Path == "" {
		c.ProtocolConfig.Path = "/motion"
	}
	timeout := 3 * time.Second
	if c.ProtocolConfig.Timeout != "" {
		if d, err := time.ParseDuration(c.ProtocolConfig.Timeout); err == nil {
			timeout = d
		}
	}
	_ = timeout

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	// If your go-coap/v3 version doesn’t have udp.WithTarget, use the older signature:
	conn, err := udp.Dial(c.ProtocolConfig.Addr)
	if err != nil {
		return fmt.Errorf("coap dial %s: %w", c.ProtocolConfig.Addr, err)
	}
	c.conn = conn

	c.deviceMutex.Lock()
	c.isConnected = true
	if c.motionStatus == "" {
		c.motionStatus = "no_motion"
	}
	c.deviceMutex.Unlock()

	klog.Infof("CoAP connected successfully to %s", c.ProtocolConfig.Addr)

	if c.ProtocolConfig.Observe {
		_, err := conn.Observe(ctx, c.ProtocolConfig.Path, func(m *pool.Message) {
			payload, _ := m.ReadBody()
			val := string(payload)
			if val == "" {
				val = "no_motion"
			}
			c.deviceMutex.Lock()
			old := c.motionStatus
			c.motionStatus = val
			c.deviceMutex.Unlock()
			if old != val {
				klog.Infof("CoAP observe: motion changed %q -> %q", old, val)
			}
		})
		if err != nil {
			return fmt.Errorf("observe %s: %w", c.ProtocolConfig.Path, err)
		}
	}

	if prev != "" {
		c.motionStatus = prev
	}
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

func (c *CustomizedClient) GetDeviceData(visitor *VisitorConfig) (interface{}, error) {
	name := visitor.VisitorConfigData.PropertyName

	switch name {
	case "motion":
		if c.ProtocolConfig.Observe {
			c.deviceMutex.Lock()
			val := c.motionStatus
			c.deviceMutex.Unlock()
			return val, nil
		}

		if c.conn == nil {
			return "no_motion", fmt.Errorf("coap not connected")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		resp, err := c.conn.Get(ctx, c.ProtocolConfig.Path)
		if err != nil {
			klog.Warningf("CoAP GET failed: %v", err)
			c.deviceMutex.Lock()
			val := c.motionStatus
			c.deviceMutex.Unlock()
			return val, nil
		}
		if resp.Code() != codes.Content {
			klog.Warningf("CoAP GET code %v", resp.Code())
			c.deviceMutex.Lock()
			val := c.motionStatus
			c.deviceMutex.Unlock()
			return val, nil
		}
		body, _ := resp.ReadBody()
		val := string(body)
		if val == "" {
			val = "no_motion"
		}
		c.deviceMutex.Lock()
		c.motionStatus = val
		c.deviceMutex.Unlock()
		return val, nil

	default:
		return nil, fmt.Errorf("unknown property %q", name)
	}
}

func (c *CustomizedClient) DeviceDataWrite(visitor *VisitorConfig, deviceMethodName string, propertyName string, data interface{}) error {
        // Motion detection is typically read-only, but we can implement this for completeness
        return nil
}

func (c *CustomizedClient) SetDeviceData(data interface{}, visitor *VisitorConfig) error {
        // Motion detection is typically read-only from the device perspective
        return nil
}


func (c *CustomizedClient) GetDeviceStates() (string, error) {
    c.deviceMutex.Lock()
    connected := c.isConnected && c.conn != nil
    c.deviceMutex.Unlock()

    if connected {
        return common.DeviceStatusOK, nil
    }
    return common.DeviceStatusDisCONN, nil
}

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
func ParseVisitorConfigFromGrpc(visitor *v1beta1.VisitorConfig) (VisitorConfig, error) {
    visitorConfig := VisitorConfig{}
    visitorConfig.ProtocolName = visitor.ProtocolName
    if visitor.ConfigData != nil {
        if err := json.Unmarshal(visitor.ConfigData, &visitorConfig.VisitorConfigData); err != nil {
            return visitorConfig, err
        }
    }
    return visitorConfig, nil
}
