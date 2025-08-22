package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
		motion:         false,
		lastDetected:   "",
		class:          "",
		isConnected:    false,
	}
	return client, nil
}

func (c *CustomizedClient) InitDevice() error {
	klog.Infof("Init CoAP device addr=%s paths=[%s %s %s]",
		c.ConfigData.Addr, c.ConfigData.MotionPath, c.ConfigData.LastPath, c.ConfigData.ClassPath)

	if c.ProtocolConfig.Addr == "" {
		return fmt.Errorf("addr is required in protocol config")
	}
	if c.ProtocolConfig.MotionPath == "" {
		c.ProtocolConfig.MotionPath = "/motion"
	}

	if c.ProtocolConfig.LastPath == "" {
		c.ProtocolConfig.LastPath = "/last_detection"
	}
	if c.ProtocolConfig.ClassPath == "" {
                c.ProtocolConfig.ClassPath = "/class"
        }


	// parent context for the client lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	// launch the self-healing loop (will dial, observe, health-check, and reconnect)
	go c.runConnectionLoop(ctx)

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
		c.deviceMutex.Unlock()
		klog.Infof("CoAP connected successfully to %s", c.ProtocolConfig.Addr)
		backoff = minBackoff

		// Set up Observe if enabled
		obsCancels := []context.CancelFunc{}
		setupObs := func(path string, handler func(*pool.Message)) error {
			obsCtx, cancel := context.WithCancel(ctx)
			obsCancels = append(obsCancels, cancel)
			_, err := conn.Observe(obsCtx, path, handler)
			return err
		}

		if c.ProtocolConfig.ObserveMotion {
			if err := setupObs(c.ProtocolConfig.MotionPath, func(m *pool.Message) {
				val := parseBoolPayload(m)
				c.deviceMutex.Lock()
				old := c.motion
				c.motion = val
				c.deviceMutex.Unlock()
				if old != val {
					klog.Infof("CoAP observe motion: %v", val)
				}
			}); err != nil {
				klog.Warningf("Observe %s failed: %v", c.ProtocolConfig.MotionPath, err)
			} else {
				klog.Infof("Observing %s", c.ProtocolConfig.MotionPath)
			}
		}

		if c.ProtocolConfig.ObserveLast {
			if err := setupObs(c.ProtocolConfig.LastPath, func(m *pool.Message) {
				body, _ := m.ReadBody()
				val := strings.TrimSpace(string(body))
				c.deviceMutex.Lock()
				c.lastDetected = val
				c.deviceMutex.Unlock()
				klog.Infof("CoAP observe last_detected: %s", val)
			}); err != nil {
				klog.Warningf("Observe %s failed: %v", c.ProtocolConfig.LastPath, err)
			} else {
				klog.Infof("Observing %s", c.ProtocolConfig.LastPath)
			}
		}

		if c.ProtocolConfig.ObserveClass {
			if err := setupObs(c.ProtocolConfig.ClassPath, func(m *pool.Message) {
				body, _ := m.ReadBody()
				val := strings.TrimSpace(string(body))
				c.deviceMutex.Lock()
				c.class = val
				c.deviceMutex.Unlock()
				klog.Infof("CoAP observe class: %s", val)
			}); err != nil {
				klog.Warningf("Observe %s failed: %v", c.ProtocolConfig.ClassPath, err)
			} else {
				klog.Infof("Observing %s", c.ProtocolConfig.ClassPath)
			}
		}

		// Health-check loop
		healthTicker := time.NewTicker(healthInterval)
		ok := true
		for ok {
			select {
			case <-ctx.Done():
				healthTicker.Stop()
				for _, cancel := range obsCancels {
					cancel()
				}
				return
			case <-healthTicker.C:
				hctx, cancel := context.WithTimeout(ctx, healthTimeout)
				_, err := conn.Get(hctx, c.ProtocolConfig.MotionPath)
				cancel()
				if err != nil {
					klog.Warningf("CoAP health check failed: %v (will reconnect)", err)
					ok = false
				}
			}
		}

		// Leave observe, close connection, backoff, then retry
		for _, cancel := range obsCancels {
			cancel()
		}
		c.closeConn()
		if !c.sleepOrExit(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
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

func parseBoolPayload(m *pool.Message) bool {
	body, _ := m.ReadBody()
	s := strings.TrimSpace(strings.ToLower(string(body)))
	switch s {
	case "true", "1", "on", "yes", "y", "motion", "motion_detected":
		return true
	case "false", "0", "off", "no", "n", "no_motion":
		return false
	default:
		// best-effort: try to parse JSON "true"/"false"
		b, err := strconv.ParseBool(s)
		if err == nil {
			return b
		}
		return false
	}
}


// GetDeviceData returns device data for a specific property
func (c *CustomizedClient) GetDeviceData(visitor *VisitorConfig) (interface{}, error) {
	prop := visitor.VisitorConfigData.PropertyName
	klog.V(2).Infof("GetDeviceData called for property: %s", prop)
	c.deviceMutex.Lock()
	defer c.deviceMutex.Unlock()

	switch prop {
	case "motion":
		// If observe enabled, just return cached state.
		if c.ProtocolConfig.ObserveMotion && c.conn != nil {
			if v, ok := c.pollBool(c.ProtocolConfig.MotionPath); ok {
				c.motion = v
			}
		}
		return c.motion, nil

	case "last_detection":
		if !c.ProtocolConfig.ObserveLast && c.conn != nil {
			if v, ok := c.pollString(c.ProtocolConfig.LastPath); ok {
				c.lastDetected = v
			}
		}
		return c.lastDetected, nil

        case "class":
                if !c.ProtocolConfig.ObserveClass && c.conn != nil {
                        if v, ok := c.pollString(c.ProtocolConfig.ClassPath); ok {
                                c.class = v
                        }
                }
                return c.class, nil
	default:
		return nil, fmt.Errorf("unknown property: %s", prop)
	}
}

func (c *CustomizedClient) pollBool(path string) (bool, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), getTimeout)
	defer cancel()
	if c.conn == nil {
		return false, false
	}
	resp, err := c.conn.Get(ctx, path)
	if err != nil || resp.Code() != codes.Content {
		return false, false
	}
	return parseBoolPayload(resp), true
}

func (c *CustomizedClient) pollString(path string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), getTimeout)
	defer cancel()
	if c.conn == nil {
		return "", false
	}
	resp, err := c.conn.Get(ctx, path)
	if err != nil || resp.Code() != codes.Content {
		return "", false
	}
	body, _ := resp.ReadBody()
	return strings.TrimSpace(string(body)), true
}


/*func cachedOrNoMotion(cached string) string {
	if cached == "" {
		return "no_motion"
	}
	return cached
}*/

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
