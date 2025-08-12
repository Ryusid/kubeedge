package device

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeedge/mqtt/driver"
	dmiapi "github.com/kubeedge/api/apis/dmi/v1beta1"
	"github.com/kubeedge/mapper-framework/pkg/common"
	"github.com/kubeedge/mapper-framework/pkg/grpcclient"
	"github.com/kubeedge/mapper-framework/pkg/util/parse"
)

type TwinData struct {
	DeviceName      string
	DeviceNamespace string
	Client          *driver.CustomizedClient
	Name            string
	Type            string
	ObservedDesired common.TwinProperty
	VisitorConfig   *driver.VisitorConfig
	Topic           string
	Results         interface{}
	CollectCycle    time.Duration
	ReportToCloud   bool
}

func (td *TwinData) GetPayLoad() ([]byte, error) {
	var err error
	td.VisitorConfig.VisitorConfigData.DataType = strings.ToLower(td.VisitorConfig.VisitorConfigData.DataType)
	
	klog.V(2).Infof("GetPayLoad calling GetDeviceData for property %s", td.Name)
	td.Results, err = td.Client.GetDeviceData(td.VisitorConfig)
	if err != nil {
		return nil, fmt.Errorf("get device data failed: %v", err)
	}
	
	klog.V(2).Infof("GetDeviceData returned for property %s: %v", td.Name, td.Results)
	
	sData, err := common.ConvertToString(td.Results)
	if err != nil {
		klog.Errorf("Failed to convert %s %s value as string : %v", td.DeviceName, td.Name, err)
		return nil, err
	}
	if len(sData) > 30 {
		klog.V(4).Infof("Get %s : %s ,value is %s......", td.DeviceName, td.Name, sData[:30])
	} else {
		klog.V(2).Infof("Get %s : %s ,value is %s", td.DeviceName, td.Name, sData)
	}
	var payload []byte
	if strings.Contains(td.Topic, "$hw") {
		if payload, err = common.CreateMessageTwinUpdate(td.Name, td.Type, sData, td.ObservedDesired.Value); err != nil {
			return nil, fmt.Errorf("create message twin update failed: %v", err)
		}
	} else {
		if payload, err = common.CreateMessageData(td.Name, td.Type, sData); err != nil {
			return nil, fmt.Errorf("create message data failed: %v", err)
		}
	}
	return payload, nil
}

func (td *TwinData) PushToEdgeCore() {
	klog.V(2).Infof("PushToEdgeCore called for property %s", td.Name)
	payload, err := td.GetPayLoad()
	if err != nil {
		klog.Errorf("twindata %s getPayLoad failed, err: %s", td.Name, err)
		return
	}

	klog.V(2).Infof("Generated payload for property %s: %s", td.Name, string(payload))

	var msg common.DeviceTwinUpdate
	if err = json.Unmarshal(payload, &msg); err != nil {
		klog.Errorf("twindata %s unmarshal failed, err: %s", td.Name, err)
		return
	}

	twins := parse.ConvMsgTwinToGrpc(msg.Twin)

	var rdsr = &dmiapi.ReportDeviceStatusRequest{
		DeviceName:      td.DeviceName,
		DeviceNamespace: td.DeviceNamespace,
		ReportedDevice: &dmiapi.DeviceStatus{
			Twins: twins,
			//State: "OK",
		},
	}

	klog.Infof("Reporting device status for %s/%s property %s with value: %v", td.DeviceNamespace, td.DeviceName, td.Name, msg.Twin)
	if err := grpcclient.ReportDeviceStatus(rdsr); err != nil {
		klog.Errorf("fail to report device status of %s with err: %+v", rdsr.DeviceName, err)
	} else {
		klog.V(2).Infof("Successfully reported device status for %s property %s", td.DeviceName, td.Name)
	}
}

func (td *TwinData) Run(ctx context.Context) {
	klog.Infof("TwinData.Run starting for property %s, ReportToCloud: %v, CollectCycle: %v", td.Name, td.ReportToCloud, td.CollectCycle)
	
	if !td.ReportToCloud {
		klog.Infof("TwinData.Run exiting early - ReportToCloud is false for property %s", td.Name)
		return
	}
	if td.CollectCycle == 0 {
		td.CollectCycle = common.DefaultCollectCycle
		klog.Infof("TwinData.Run using default CollectCycle %v for property %s", td.CollectCycle, td.Name)
	}
	
	klog.Infof("TwinData.Run starting ticker with cycle %v for property %s", td.CollectCycle, td.Name)
	ticker := time.NewTicker(td.CollectCycle)
	for {
		select {
		case <-ticker.C:
			klog.V(3).Infof("TwinData.Run ticker fired for property %s, calling PushToEdgeCore", td.Name)
			td.PushToEdgeCore()
		case <-ctx.Done():
			klog.Infof("TwinData.Run context cancelled for property %s", td.Name)
			return
		}
	}
}
