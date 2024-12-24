package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/candevice"
	"go.einride.tech/can/pkg/socketcan"
	"gopkg.in/yaml.v3"
)

const (
	period time.Duration = 100
)

type Config struct {
	OtaAndIg map[string]struct {
		CycleTime  int                     `yaml:"cycleTime"`
		SendId     uint32                  `yaml:"sendId"`
		SendLength uint8                   `yaml:"sendLength"`
		SendValue  [can.MaxDataLength]byte `yaml:"sendValue"`
	} `yaml:"otaAndIg"`
}

var config Config = Config{}

// 监控 OTA模式请求 循环发送 模拟车辆控制为OTA模式
// OTA
func otaCanToCan1(inputCan, outputCan string) {
	var wg sync.WaitGroup

	inputConn, err := socketcan.DialContext(context.Background(), "can", inputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", inputCan, err)
	}
	defer inputConn.Close()

	outputConn, err := socketcan.DialContext(context.Background(), "can", outputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", outputCan, err)
	}
	defer outputConn.Close()

	// 初始化响应类型为 0x00
	currentResponseType := byte(0x00)
	responseTypeChan := make(chan byte)
	// 周期性发送响应报文
	wg.Add(1)
	go func() {
		defer wg.Done()
		CycleTime := config.OtaAndIg["VCU_VehicleCtrlMod0"].CycleTime
		responseMsg := can.Frame{
			ID:         config.OtaAndIg["VCU_VehicleCtrlMod0"].SendId,
			Length:     config.OtaAndIg["VCU_VehicleCtrlMod0"].SendLength,
			Data:       config.OtaAndIg["VCU_VehicleCtrlMod0"].SendValue,
			IsExtended: true,
		}

		tx := socketcan.NewTransmitter(outputConn)
		for {
			select {
			case newResponseType := <-responseTypeChan:
				currentResponseType = newResponseType
			default:
			}
			switch currentResponseType {
			case 0x00:
				responseMsg.Data = config.OtaAndIg["VCU_VehicleCtrlMod0"].SendValue
				responseMsg.ID = config.OtaAndIg["VCU_VehicleCtrlMod0"].SendId
				responseMsg.Length = config.OtaAndIg["VCU_VehicleCtrlMod0"].SendLength
				CycleTime = config.OtaAndIg["VCU_VehicleCtrlMod0"].CycleTime
			case 0x02:
				responseMsg.Data = config.OtaAndIg["VCU_VehicleCtrlMod1"].SendValue
				responseMsg.ID = config.OtaAndIg["VCU_VehicleCtrlMod1"].SendId
				responseMsg.Length = config.OtaAndIg["VCU_VehicleCtrlMod1"].SendLength
				CycleTime = config.OtaAndIg["VCU_VehicleCtrlMod1"].CycleTime
			default:
				log.Println("Unknown response type")
			}

			if err := tx.TransmitFrame(context.Background(), responseMsg); err != nil {
				log.Printf("Error transmitting response frame: %v", err)
			}
			time.Sleep(time.Duration(CycleTime) * time.Millisecond)
		}
	}()

	// 一直收报文
	log.Println("Listening for messages on", inputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()

			// 检查是否是 OTA 报文
			if receiveMsg.ID == 0x18FF4D4A {
				data := fmt.Sprintf("%02X", receiveMsg.Data[0])
				if data == "02" {
					log.Println("Receive OTA entry request signal 02, send simulated vehicle control to OTA mode")
					responseTypeChan <- 0x02
				} else if data == "00" {
					log.Println("Receive OTA exit request signal 00, send simulated vehicle control to idle/local mode")
					responseTypeChan <- 0x00
				} else {
					log.Println("Unknown OTA signal received, request to enter vehicle control failed")
				}
			}
		}
	}()

	wg.Wait()
}

// 监控setIG状态状态并周期发送模拟车辆状态 单ID双状态
func igCanToCan(inputCan, outputCan string) {
	var wg sync.WaitGroup

	inputConn, err := socketcan.DialContext(context.Background(), "can", inputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", inputCan, err)
	}
	defer inputConn.Close()

	outputConn, err := socketcan.DialContext(context.Background(), "can", outputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", outputCan, err)
	}
	defer outputConn.Close()

	// 初始化响应类型为 0x05
	currentResponseType := byte(0x4)
	responseTypeChan := make(chan byte)
	// 周期性发送响应报文
	wg.Add(1)
	go func() {
		defer wg.Done()
		CycleTime := config.OtaAndIg["BCM_PowerState0"].CycleTime
		responseMsg := can.Frame{
			ID:         config.OtaAndIg["BCM_PowerState0"].SendId,
			Length:     config.OtaAndIg["BCM_PowerState0"].SendLength,
			Data:       config.OtaAndIg["BCM_PowerState0"].SendValue,
			IsExtended: true,
		}

		tx := socketcan.NewTransmitter(outputConn)
		for {
			select {
			case newResponseType := <-responseTypeChan:
				currentResponseType = newResponseType
			default:
			}
			switch currentResponseType {
			case 0x04:
				responseMsg.Data = config.OtaAndIg["BCM_PowerState0"].SendValue
				responseMsg.ID = config.OtaAndIg["BCM_PowerState0"].SendId
				responseMsg.Length = config.OtaAndIg["BCM_PowerState0"].SendLength
				CycleTime = config.OtaAndIg["BCM_PowerState0"].CycleTime
			case 0x10:
				responseMsg.Data = config.OtaAndIg["BCM_PowerState1"].SendValue
				responseMsg.ID = config.OtaAndIg["BCM_PowerState1"].SendId
				responseMsg.Length = config.OtaAndIg["BCM_PowerState1"].SendLength
				CycleTime = config.OtaAndIg["BCM_PowerState1"].CycleTime
			default:
				log.Println("Unknown response type")
			}

			if err := tx.TransmitFrame(context.Background(), responseMsg); err != nil {
				log.Printf("Error transmitting response frame: %v", err)
			}
			time.Sleep(time.Duration(CycleTime) * time.Millisecond)
		}
	}()

	// 一直收报文
	log.Println("Listening for messages on", inputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()

			// 检查是否是 IG 报文
			if receiveMsg.ID == 0x18FF084A {
				data := fmt.Sprintf("%02X", receiveMsg.Data[0])
				//模拟车辆状态为IG ON
				if data == "04" || data == "05" {
					log.Println("B0 byte is 04/05, the simulated vehicle status is IG ON")
					responseTypeChan <- 0x04
				} else if data == "10" {
					//模拟车辆状态为IG OFF
					log.Println("B0 byte is 10, the simulated vehicle status is IG OFF")
					responseTypeChan <- 0x10
				}
			}
		}
	}()

	wg.Wait()
}

// 监控循环发送防盗认证报文
func CanToCan2(inputCan, outputCan string) {
	var wg sync.WaitGroup
	msgChan := make(chan struct{})
	inputConn, err := socketcan.DialContext(context.Background(), "can", inputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", inputCan, err)
	}
	defer inputConn.Close()

	outputConn, err := socketcan.DialContext(context.Background(), "can", outputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", outputCan, err)
	}
	defer outputConn.Close()

	// receive
	log.Println("Listening for messages on", inputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()
			if receiveMsg.ID == 0x18FF0A4A &&
				receiveMsg.Data == [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00} {
				msgChan <- struct{}{}
			}
		}
	}()

	// send
	log.Println("Sending messages from", inputCan, "to", outputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		responseMsg := can.Frame{
			ID:         0x18F10127,
			Length:     8,
			Data:       [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00},
			IsExtended: true,
		}
		for {
			<-msgChan
			tx := socketcan.NewTransmitter(outputConn)
			for i := 0; i < 3; i++ {
				if err := tx.TransmitFrame(context.Background(), responseMsg); err != nil {
					log.Printf("Error transmitting response frame: %v", err)
				}
				time.Sleep(period * time.Millisecond)
			}
		}
	}()

	wg.Wait()
}

func main() {
	dataBytes, err := os.ReadFile("/data/configl4.yaml")
	if err != nil {
		fmt.Println("read config file failed", err)
		return
	}
	err = yaml.Unmarshal(dataBytes, &config)
	if err != nil {
		fmt.Println("解析 yaml 文件失败：", err)
		return
	}

	d, _ := candevice.New("can1")
	_ = d.SetBitrate(250000)
	_ = d.SetUp()

	//监控ota请求报文 并周期发送模拟车辆为OTA模式  //监控退出ota请求报文 并周期发送模拟车辆为空闲，本地模式
	go otaCanToCan1("can1", "can1")

	//监控setIG状态状态并周期发送模拟车辆状态 单ID双状态
	go igCanToCan("can1", "can1")

	//监控并周期发送防盗报文
	go CanToCan2("can1", "can1")

	// 防止主 goroutine 退出
	select {}

}
