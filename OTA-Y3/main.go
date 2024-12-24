package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/socketcan"
	"gopkg.in/yaml.v3"
)

// period代表毫秒数
const (
	period time.Duration = 100
)

type Config struct {
	CycleMsg map[string]struct {
		CycleTime  int                     `yaml:"cycleTime"`
		SendId     uint32                  `yaml:"sendId"`
		SendLength uint8                   `yaml:"sendLength"`
		SendValue  [can.MaxDataLength]byte `yaml:"sendValue"`
	} `yaml:"cycleMsg"`
	OtaAndIg map[string]struct {
		CycleTime  int                     `yaml:"cycleTime"`
		SendId     uint32                  `yaml:"sendId"`
		SendLength uint8                   `yaml:"sendLength"`
		SendValue  [can.MaxDataLength]byte `yaml:"sendValue"`
	} `yaml:"otaAndIg"`
}

var config Config = Config{}

// 脚本开始运行，模拟igon信号
func cycleCan1(outputCan string) {
	//周期性的发报文
	outputConn, err := socketcan.DialContext(context.Background(), "can", outputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", outputCan, err)
	}
	defer outputConn.Close()

	// 周期性发送响应报文
	responseMsg := can.Frame{
		ID:         0x18FF0121,
		Length:     8,
		Data:       [8]byte{0xD1, 0x28, 0xCD, 0xA5, 0x6D, 0x66, 0x25, 0x50},
		IsExtended: true,
	}
	tx := socketcan.NewTransmitter(outputConn)
	for {
		if err := tx.TransmitFrame(context.Background(), responseMsg); err != nil {
			log.Printf("Error transmitting response frame: %v", err)
		}
		time.Sleep(period * time.Millisecond)
	}
}

// 将config中的CycleMsg周期发送
func cycleCan(outputCan string) {
	outputConn, err := socketcan.DialContext(context.Background(), "can", outputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", outputCan, err)
	}
	defer outputConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 为每个报文启动一个 Goroutine
	for name, msgConfig := range config.CycleMsg {
		go func(name string, msgConfig struct {
			CycleTime  int                     `yaml:"cycleTime"`
			SendId     uint32                  `yaml:"sendId"`
			SendLength uint8                   `yaml:"sendLength"`
			SendValue  [can.MaxDataLength]byte `yaml:"sendValue"`
		}) {
			tx := socketcan.NewTransmitter(outputConn)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					msg := can.Frame{
						ID:         msgConfig.SendId,
						Length:     msgConfig.SendLength,
						Data:       msgConfig.SendValue,
						IsExtended: true,
					}
					if err := tx.TransmitFrame(context.Background(), msg); err != nil {
						log.Printf("Error transmitting frame %s: %v", name, err)
					}
					time.Sleep(time.Duration(msgConfig.CycleTime) * time.Millisecond)
				}
			}
		}(name, msgConfig)
	}

	// 主 Goroutine 阻塞，等待所有子 Goroutine 完成
	select {}
}

// 监控文件收到“enter state [execute_user_confirm]”字段时，开始发送下面信号
// 下载完成，等待用户确认升级，发送策略满足信号
func cycleCan2(outputCan string) {
	// 周期性的发报文
	outputConn, err := socketcan.DialContext(context.Background(), "can", outputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", outputCan, err)
	}
	defer outputConn.Close()

	// 周期发送的报文的切片
	var responseMsgs = []can.Frame{
		{
			ID:         0x18FEF100,
			Length:     8,
			Data:       [8]byte{0x04, 0x00, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00},
			IsExtended: true,
		},
		{
			ID:         0x18F101D0,
			Length:     8,
			Data:       [8]byte{0x60, 0x05, 0x01, 0x51, 0x00, 0x00, 0x00, 0x01},
			IsExtended: true,
		},
		{
			ID:         0x18F802F4,
			Length:     8,
			Data:       [8]byte{0x0A, 0xC8, 0x06, 0x42, 0x27, 0x96, 0xC0, 0x8C},
			IsExtended: true,
		},
		{
			ID:         0x18FF0727,
			Length:     8,
			Data:       [8]byte{0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			IsExtended: true,
		},
	}

	// 周期性发送响应报文
	tx := socketcan.NewTransmitter(outputConn)
	for {
		for _, msg := range responseMsgs {
			if err := tx.TransmitFrame(context.Background(), msg); err != nil {
				log.Printf("Error transmitting response frame: %v", err)
			}
		}
		time.Sleep(period * time.Millisecond)
	}
}

// 需要200ms发送的报文和上面的分开发送
func cycleCan3(outputCan string) {
	//周期性的发报文
	outputConn, err := socketcan.DialContext(context.Background(), "can", outputCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", outputCan, err)
	}
	defer outputConn.Close()

	// 周期性发送响应报文
	responseMsg := can.Frame{
		ID:         0x18F804F4,
		Length:     8,
		Data:       [8]byte{0x88, 0xF3, 0x01, 0xFF, 0x02, 0x23, 0x24, 0x01},
		IsExtended: true,
	}
	tx := socketcan.NewTransmitter(outputConn)
	for {
		if err := tx.TransmitFrame(context.Background(), responseMsg); err != nil {
			log.Printf("Error transmitting response frame: %v", err)
		}
		time.Sleep(2 * period * time.Millisecond)
	}
}

// 监控并发送防盗认证报文
func CanToCan1(inputCan, outputCan string) {
	var wg sync.WaitGroup
	msgChan := make(chan can.Frame, 1000)
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

	//收报文并发送一条
	log.Println("Listening for messages on", inputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()
			if receiveMsg.ID == 0x18FF0A4A {
				data1 := fmt.Sprintf("%02X", receiveMsg.Data[0])
				data2 := fmt.Sprintf("%02X", receiveMsg.Data[1])
				//监控B0,B1字节报文，发送模拟防盗认证报文
				if data1 == "00" && data2 == "00" {
					msgChan <- can.Frame{
						ID:         0x18FF0927,
						Length:     8,
						Data:       [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
						IsExtended: true,
					}
				}
			}
		}
	}()
	// send
	log.Println("Sending messages to", outputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for sendMsg := range msgChan {
			tx := socketcan.NewTransmitter(outputConn)
			if err := tx.TransmitFrame(context.Background(), sendMsg); err != nil {
				log.Printf("Error transmitting frame to %s: %v", outputCan, err)
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
			if receiveMsg.ID == 0x18FF0A4A {
				//监控B0,B1字节报文，发送模拟防盗认证报文
				data1 := fmt.Sprintf("%02X", receiveMsg.Data[2])
				data2 := fmt.Sprintf("%02X", receiveMsg.Data[3])
				if data1 == "FF" && data2 == "28" {
					msgChan <- struct{}{}
				}
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
			Data:       [8]byte{0x00, 0x00, 0x00, 0x00, 0x20, 0x20, 0x05, 0x01},
			IsExtended: true,
		}
		<-msgChan
		tx := socketcan.NewTransmitter(outputConn)
		for {
			if err := tx.TransmitFrame(context.Background(), responseMsg); err != nil {
				log.Printf("Error transmitting response frame: %v", err)
			}
			time.Sleep(period * time.Millisecond)
		}
	}()

	wg.Wait()
}

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
				} else if data == "01" {
					log.Fatal("Received OTA signal 01, failed to report vehicle control")
				} else {
					log.Fatal("Unknown OTA signal received, request to enter vehicle control failed")
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
	currentResponseType := byte(0x10)
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
				responseMsg.Data = config.OtaAndIg["BCM_PowerState1"].SendValue
				responseMsg.ID = config.OtaAndIg["BCM_PowerState1"].SendId
				responseMsg.Length = config.OtaAndIg["BCM_PowerState1"].SendLength
				CycleTime = config.OtaAndIg["BCM_PowerState1"].CycleTime
			case 0x10:
				responseMsg.Data = config.OtaAndIg["BCM_PowerState0"].SendValue
				responseMsg.ID = config.OtaAndIg["BCM_PowerState0"].SendId
				responseMsg.Length = config.OtaAndIg["BCM_PowerState0"].SendLength
				CycleTime = config.OtaAndIg["BCM_PowerState0"].CycleTime
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
				if data == "04" {
					log.Println("B0 byte is 04, the simulated vehicle status is IG ON")
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

// 监控日志文件
func watchLogFile(filePath string, trigger string, startFunc func()) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, trigger) {
			fmt.Println("Trigger detected, starting goroutines...")
			startFunc()
			break
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading file: %v\n", err)
	}
}

func main() {

	dataBytes, err := os.ReadFile("/data/config.yaml")
	if err != nil {
		fmt.Println("read config file failed", err)
		return
	}
	err = yaml.Unmarshal(dataBytes, &config)
	if err != nil {
		fmt.Println("解析 yaml 文件失败：", err)
		return
	}

	//监控日志文件路径
	//logFilePath := "/mnt/ota/hisync_fota/logs/hr_agent_service.log"
	//监控日志文件语句
	//triggerString := "enter state [execute_user_confirm]"
	// 定义启动goroutines的函数
	//startGoroutines := func() {
	//监控ota请求报文 并周期发送模拟车辆为OTA模式  //监控退出ota请求报文 并周期发送模拟车辆为空闲，本地模式
	//go otaCanToCan1("can0", "can0")
	//监控setIG状态状态并周期发送模拟车辆状态 单ID双状态
	//go igCanToCan("can0", "can0")
	// //100ms周期发送报文
	// go cycleCan2("can0")
	// //200ms周期发送报文
	// go cycleCan3("can0")
	//go cycleCan("can0")
	//}
	//100ms周期发送igon报文
	//go cycleCan1("can0")

	//监控并回复单报文
	go CanToCan1("can2", "can2")

	//监控并周期发送单报文
	go CanToCan2("can2", "can2")

	// 开始监控日志文件(下载完成，等待用户确认升级，发送策略满足信号)
	//go watchLogFile(logFilePath, triggerString, startGoroutines)

	//监控ota请求报文 并周期发送模拟车辆为OTA模式  //监控退出ota请求报文 并周期发送模拟车辆为空闲，本地模式
	go otaCanToCan1("can2", "can2")
	//监控setIG状态状态并周期发送模拟车辆状态 单ID双状态
	go igCanToCan("can2", "can2")
	// //100ms周期发送报文
	// go cycleCan2("can0")
	// //200ms周期发送报文
	// go cycleCan3("can0")
	go cycleCan("can2")

	// 防止主 goroutine 退出
	select {}
}
