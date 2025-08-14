package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/message/pool"
	"github.com/plgd-dev/go-coap/v3/udp"
	"k8s.io/klog/v2"

	"github.com/kubeedge/api/apis/devices/v1beta1"
	"github.com/kubeedge/mapper-framework/pkg/common"
)

const (
	minBackoff     = 1 * time.Second
	maxBackoff     = 30 * time.Second
	healthInterval = 10 * time.Second
	healthTimeout  = 1 * time.Second
	getTimeout     = 3 * time.Second
)

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
	prev := c.motionStatus
	klog.Infof("Initializing CoAP device with addr: %s (preserving state: %s)", c.ProtocolConfig.Addr, prev)

	if c.ProtocolConfig.Addr == "" {
		return fmt.Errorf("addr is required in protocol config")
	}
	if c.ProtocolConfig.Path == "" {
		c.ProtocolConfig.Path = "/motion"
	}

	// parent context for the client lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	// launch the self-healing loop (will dial, observe, health-check, and reconnect)
	go c.runConnectionLoop(ctx)

	// restore cached state (if any)
	if prev != "" {
		c.motionStatus = prev
	}
	return nil
}

func (c *CustomizedClient) StopDevice() error {
	klog.Infof("Stopping CoAP device")
	if c.cancel != nil {
		c.cancel()
	}
	c.closeConn()
	klog.Infof("CoAP client disconnected")
	return nil
}

// Self-healing loop: dial -> (optional) observe -> health-check -> reconnect on failure
func (c *CustomizedClient) runConnectionLoop(ctx context.Context) {
	backoff := minBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		// Dial
		conn, err := udp.Dial(c.ProtocolConfig.Addr)
		if err != nil {
			klog.Warningf("CoAP dial %s failed: %v", c.ProtocolConfig.Addr, err)
			if !c.sleepOrExit(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		c.deviceMutex.Lock()
		c.conn = conn
		c.isConnected = true
		if c.motionStatus == "" {
			c.motionStatus = "no_motion"
		}
		c.deviceMutex.Unlock()
		klog.Infof("CoAP connected successfully to %s", c.ProtocolConfig.Addr)
		backoff = minBackoff

		// Set up Observe if enabled
		var obsCancel context.CancelFunc
		if c.ProtocolConfig.Observe {
			obsCtx, cancel := context.WithCancel(ctx)
			obsCancel = cancel

			_, err := conn.Observe(obsCtx, c.ProtocolConfig.Path, func(n *pool.Message) {
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
					klog.Infof("CoAP observe notification: '%s' -> '%s'", old, val)
				}
			})
			if err != nil {
				klog.Warningf("Observe %s failed: %v", c.ProtocolConfig.Path, err)
				// observation failed; tear down and reconnect
				if obsCancel != nil {
					obsCancel()
				}
				c.closeConn()
				if !c.sleepOrExit(ctx, backoff) {
					return
				}
				backoff = nextBackoff(backoff)
				continue
			}
			klog.Infof("Observing CoAP resource: %s", c.ProtocolConfig.Path)
		}

		// Health-check loop
		healthTicker := time.NewTicker(healthInterval)
		ok := true
		for ok {
			select {
			case <-ctx.Done():
				healthTicker.Stop()
				if obsCancel != nil {
					obsCancel()
				}
				return
			case <-healthTicker.C:
				hctx, cancel := context.WithTimeout(ctx, healthTimeout)
				_, err := conn.Get(hctx, c.ProtocolConfig.Path)
				cancel()
				if err != nil {
					klog.Warningf("CoAP health check failed: %v (will reconnect)", err)
					ok = false
				}
			}
		}

		// Leave observe, close connection, backoff, then retry
		if obsCancel != nil {
			obsCancel()
		}
		c.closeConn()
		if !c.sleepOrExit(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	nb := cur * 2
	if nb > maxBackoff {
		return maxBackoff
	}
	return nb
}

func (c *CustomizedClient) sleepOrExit(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (c *CustomizedClient) closeConn() {
	c.deviceMutex.Lock()
	defer c.deviceMutex.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.isConnected = false
}

// GetDeviceData returns device data for a specific property
func (c *CustomizedClient) GetDeviceData(visitor *VisitorConfig) (interface{}, error) {
	prop := visitor.VisitorConfigData.PropertyName
	klog.V(2).Infof("GetDeviceData called for property: %s", prop)

	switch prop {
	case "motion":
		// If observe enabled, just return cached state.
		if c.ProtocolConfig.Observe {
			c.deviceMutex.Lock()
			val := c.motionStatus
			c.deviceMutex.Unlock()
			klog.V(2).Infof("Returning cached motion status: %s", val)
			return val, nil
		}

		// Polling mode: do a live GET, with a small timeout. If it fails, return cached.
		c.deviceMutex.Lock()
		conn := c.conn
		cached := c.motionStatus
		c.deviceMutex.Unlock()

		if conn == nil {
			return cachedOrNoMotion(cached), fmt.Errorf("CoAP client not connected")
		}

		ctx, cancel := context.WithTimeout(context.Background(), getTimeout)
		defer cancel()

		resp, err := conn.Get(ctx, c.ProtocolConfig.Path)
		if err != nil {
			klog.Warningf("CoAP GET failed: %v, returning cached status", err)
			return cachedOrNoMotion(cached), nil
		}
		if resp.Code() != codes.Content {
			klog.Warningf("CoAP GET returned %v, returning cached status", resp.Code())
			return cachedOrNoMotion(cached), nil
		}

		body, _ := resp.ReadBody()
		val := string(body)
		if val == "" {
			val = "no_motion"
		}

		c.deviceMutex.Lock()
		c.motionStatus = val
		c.deviceMutex.Unlock()
		klog.V(2).Infof("CoAP GET returned motion status: %s", val)
		return val, nil

	default:
		return nil, fmt.Errorf("unknown property: %s", prop)
	}
}

func cachedOrNoMotion(cached string) string {
	if cached == "" {
		return "no_motion"
	}
	return cached
}

func (c *CustomizedClient) DeviceDataWrite(visitor *VisitorConfig, deviceMethodName string, propertyName string, data interface{}) error {
	klog.V(3).Infof("DeviceDataWrite called for property: %s with data: %v", propertyName, data)
	return nil
}

func (c *CustomizedClient) SetDeviceData(data interface{}, visitor *VisitorConfig) error {
	klog.V(3).Infof("SetDeviceData called with data: %v", data)
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

// -------- Parsing helpers (kubeedge/api v1beta1) --------

func ParseProtocolFromGrpc(protocol *v1beta1.ProtocolConfig) (ProtocolConfig, error) {
	pc := ProtocolConfig{}
	if protocol != nil && protocol.ConfigData != nil && protocol.ConfigData.Data != nil {
		b, err := json.Marshal(protocol.ConfigData.Data)
		if err != nil {
			return pc, fmt.Errorf("marshal protocol config: %w", err)
		}
		if err := json.Unmarshal(b, &pc); err != nil {
			return pc, fmt.Errorf("unmarshal protocol config: %w", err)
		}
		klog.V(2).Infof("Parsed protocol config: %+v", pc)
	}
	return pc, nil
}

func ParseVisitorConfigFromGrpc(visitor *v1beta1.VisitorConfig) (VisitorConfig, error) {
	vc := VisitorConfig{}
	if visitor == nil {
		return vc, nil
	}
	vc.ProtocolName = visitor.ProtocolName
	if visitor.ConfigData != nil && visitor.ConfigData.Data != nil {
		b, err := json.Marshal(visitor.ConfigData.Data)
		if err != nil {
			return vc, fmt.Errorf("marshal visitor config: %w", err)
		}
		if err := json.Unmarshal(b, &vc.VisitorConfigData); err != nil {
			return vc, fmt.Errorf("unmarshal visitor config: %w", err)
		}
	}
	return vc, nil
}
