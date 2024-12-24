package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/socketcan"
	"gopkg.in/yaml.v3"
)

type Config struct {
	FilterMsg map[string]struct {
		SourceCan string   `yaml:"sourceCan"`
		DesCan    string   `yaml:"desCan"`
		FilterId  []uint32 `yaml:"filterId"`
	} `yaml:"filterMsg"`
	CycleMsg map[string]struct {
		CycleTime  int                     `yaml:"cycleTime"`
		SendId     uint32                  `yaml:"sendId"`
		SendLength uint8                   `yaml:"sendLength"`
		SendValue  [can.MaxDataLength]byte `yaml:"sendValue"`
		DesCan     string                  `yaml:"desCan"`
	} `yaml:"cycleMsg"`
}

var config = Config{}

func loadConfig(filePath string) error {
	dataBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %v", err)
	}

	err = yaml.Unmarshal(dataBytes, &config)
	if err != nil {
		return fmt.Errorf("解析 YAML 文件失败: %v", err)
	}
	return nil
}

func filterCanToCan(sourceCan, desCan string, filterIds []uint32) {
	var wg sync.WaitGroup
	msgChan := make(chan can.Frame, 1000)

	inputConn, err := socketcan.DialContext(context.Background(), "can", sourceCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", sourceCan, err)
	}
	defer inputConn.Close()

	outputConn, err := socketcan.DialContext(context.Background(), "can", desCan)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", desCan, err)
	}
	defer outputConn.Close()

	// send
	log.Println("Sending messages from", sourceCan, "to", desCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for sendMsg := range msgChan {
			tx := socketcan.NewTransmitter(outputConn)
			if err := tx.TransmitFrame(context.Background(), sendMsg); err != nil {
				log.Printf("Error transmitting frame to %s: %v", desCan, err)
			}
		}
	}()

	// receive
	log.Println("Listening for messages on", sourceCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()

			// 过滤掉特定ID的报文
			if contains(filterIds, receiveMsg.ID) {
				continue
			}
			msgChan <- receiveMsg
		}
	}()
	wg.Wait()
}

func contains(ids []uint32, id uint32) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func cycleCan() {
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 为每个报文启动一个 Goroutine
	for name, msgConfig := range config.CycleMsg {
		go func(name string, msgConfig struct {
			CycleTime  int                     `yaml:"cycleTime"`
			SendId     uint32                  `yaml:"sendId"`
			SendLength uint8                   `yaml:"sendLength"`
			SendValue  [can.MaxDataLength]byte `yaml:"sendValue"`
			DesCan     string                  `yaml:"desCan"`
		}) {
			outputConn, err := socketcan.DialContext(context.Background(), "can", msgConfig.DesCan)
			if err != nil {
				log.Fatalf("Failed to open %s: %v", msgConfig.DesCan, err)
			}
			defer outputConn.Close()

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

// OTA
func OtaCanToCan(inputCan, outputCan string) {
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
	currentResponseType := byte(0x05)
	responseTypeChan := make(chan byte)
	// 周期性发送响应报文
	wg.Add(1)
	go func() {
		defer wg.Done()
		var responseData [8]byte
		responseMsg := can.Frame{
			ID:         0x10FB0FD8,
			Length:     8,
			Data:       responseData,
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
			case 0x09:
				responseData = [8]byte{0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
			case 0x05:
				responseData = [8]byte{0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
			default:
				log.Println("Unknown response type")
			}
			responseMsg.Data = responseData
			if err := tx.TransmitFrame(context.Background(), responseMsg); err != nil {
				log.Printf("Error transmitting response frame: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
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
					log.Println("Received OTA mode 02, switching to 09 response")
					responseTypeChan <- 0x09
				} else if data == "00" {
					log.Println("Received OTA mode 00, switching to 05 response")
					responseTypeChan <- 0x05
				}
			}
		}
	}()

	wg.Wait()
}

// IG
func IgnCanToCan(inputCan, outputCan string) {
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

	// 初始化响应类型为 0x04
	currentResponseType := byte(0x40)
	responseTypeChan := make(chan byte)
	// 周期性发送响应报文
	wg.Add(1)
	go func() {
		defer wg.Done()
		var responseData [8]byte
		responseMsg := can.Frame{
			ID:         0x10FB01D8,
			Length:     8,
			Data:       responseData,
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
				responseData = [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
			case 0x40:
				responseData = [8]byte{0x00, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
			default:
				log.Println("Unknown response type")
			}
			responseMsg.Data = responseData
			if err := tx.TransmitFrame(context.Background(), responseMsg); err != nil {
				log.Printf("Error transmitting response frame: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// receive
	log.Println("Listening for messages on", inputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()

			// 检查是否是 IGN 报文
			if receiveMsg.ID == 0x18F0E44A {
				data := fmt.Sprintf("%02X", receiveMsg.Data[0])
				if data == "01" {
					log.Println("Received IGN mode 02, switching to 09 response")
					responseTypeChan <- 0x00
				}
			}
			// 检查是否是 IGN 报文
			if receiveMsg.ID == 0x18F0E34A {
				data := fmt.Sprintf("%02X", receiveMsg.Data[0])
				if data == "04" {
					log.Println("Received IGN mode 02, switching to 09 response")
					responseTypeChan <- 0x40
				}
			}
		}
	}()

	wg.Wait()
}

func main() {
	// 加载配置文件
	if err := loadConfig("/data/config.yaml"); err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	// 启动过滤任务
	for name, filterConfig := range config.FilterMsg {
		go func(name string, filterConfig struct {
			SourceCan string   `yaml:"sourceCan"`
			DesCan    string   `yaml:"desCan"`
			FilterId  []uint32 `yaml:"filterId"`
		}) {
			log.Printf("Starting filter task for %s", name)
			filterCanToCan(filterConfig.SourceCan, filterConfig.DesCan, filterConfig.FilterId)
		}(name, filterConfig)
	}

	// 启动周期发送任务
	go cycleCan()

	//接受OTA模式的报文
	go OtaCanToCan("can1", "can2")

	//接受IGN模式的报文
	go IgnCanToCan("can2", "can2")

	// 防止主 goroutine 退出
	select {}
}

// 透传
func CanToCan(inputCan, outputCan string) {
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

	// send
	log.Println("Sending messages from", inputCan, "to", outputCan)
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

	// receive
	log.Println("Listening for messages on", inputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()
			msgChan <- receiveMsg
		}
	}()

	wg.Wait()
}

// 过滤掉特定ID的报文
func FilterCanToCan(inputCan, outputCan string) {
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

	// send
	log.Println("Sending messages from", inputCan, "to", outputCan)
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

	// receive
	log.Println("Listening for messages on", inputCan)
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()

			// 过滤掉特定ID的报文
			if receiveMsg.ID == 0x10FB01D8 || receiveMsg.ID == 0x10FB03D8 || receiveMsg.ID == 0x10FB0FD8 {
				continue
			}
			msgChan <- receiveMsg
		}
	}()
	wg.Wait()
}
