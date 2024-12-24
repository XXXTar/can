package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
	"source-crdc.hirain.com/atx-go/go-socketcan/pkg/socketcan"
)

type Config struct {
	FilterMsg map[string]struct {
		SourceCan string   `yaml:"sourceCan"`
		DesCan    string   `yaml:"desCan"`
		FilterId  []uint32 `yaml:"filterId"`
	} `yaml:"filterMsg"`

	CycleMsg map[string]struct {
		CycleTime  int    `yaml:"cycleTime"`
		SendId     uint32 `yaml:"sendId"`
		SendLength uint8  `yaml:"sendLength"`
		SendValue  []byte `yaml:"sendValue"`
		DesCan     string `yaml:"desCan"`
	} `yaml:"cycleMsg"`
	OTA struct {
		Id        uint32 `yaml:"id"`
		Status1   byte   `yaml:"status1"`
		Status2   byte   `yaml:"status2"`
		SourceCan string `yaml:"sourceCan"`
	} `yaml:"ota"`
	URL    string `yaml:"url"`
	Listen struct {
		Id        uint32 `yaml:"id"`
		SourceCan string `yaml:"sourceCan"`
	} `yaml:"listen"`
}

var config = Config{}
var cycleRunning int32 // 使用 atomic 包管理

var can1, can2 socketcan.Interface
var can1Subscribers, can2Subscribers []chan socketcan.CanFrame
var can1SubscribersMutex, can2SubscribersMutex sync.Mutex

type OtaRequest struct {
	ID   uint32 `json:"id"`
	Data byte   `json:"data"`
}

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

func initCanInterfaces() error {
	var err error
	can1, err = socketcan.NewRawInterface("can1")
	if err != nil {
		return fmt.Errorf("failed to open can1: %v", err)
	}

	can2, err = socketcan.NewRawInterface("can2")
	if err != nil {
		return fmt.Errorf("failed to open can2: %v", err)
	}

	can1Subscribers = make([]chan socketcan.CanFrame, 0)
	can2Subscribers = make([]chan socketcan.CanFrame, 0)

	return nil
}

func startReceiver(can socketcan.Interface, subscribers *[]chan socketcan.CanFrame) {
	go func() {
		for {
			frame, err := can.RecvFrame()
			if err != nil {
				log.Printf("Error receiving frame: %v", err)
				continue
			}

			// 广播报文到所有订阅者
			for _, sub := range *subscribers {
				select {
				case sub <- frame:
				default:
					// 如果通道已满，忽略本次发送
				}
			}
		}
	}()
	select {}
}

func subscribe(can string) chan socketcan.CanFrame {
	sub := make(chan socketcan.CanFrame, 1000)
	if can == "can1" {
		can1SubscribersMutex.Lock()
		defer can1SubscribersMutex.Unlock()
		can1Subscribers = append(can1Subscribers, sub)

	} else if can == "can2" {
		can2SubscribersMutex.Lock()
		defer can2SubscribersMutex.Unlock()
		can2Subscribers = append(can2Subscribers, sub)

	} else {
		log.Fatalf("Unknown CAN interface: %s", can)
	}
	return sub
}

func filterCanToCan(sourceCan, desCan string, filterIds []uint32) {
	var output socketcan.Interface
	sub := subscribe(sourceCan)

	if desCan == "can1" {
		output = can1
	} else if desCan == "can2" {
		output = can2
	} else {
		log.Fatalf("Unknown CAN interface: %s", desCan)
	}

	// send
	log.Println("Sending messages from", sourceCan, "to", desCan)
	go func() {
		for sendMsg := range sub {
			// 过滤掉特定ID的报文
			if contains(filterIds, sendMsg.ArbId) {
				continue
			}
			if err := output.SendFrame(sendMsg); err != nil {
				log.Printf("Error transmitting frame to %s: %v", desCan, err)
			}
		}
	}()
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
	var wg sync.WaitGroup
	atomic.StoreInt32(&cycleRunning, 0) // 设置 cycleRunning 为 true

	// 为每个报文启动一个 Goroutine
	for name, msgConfig := range config.CycleMsg {
		wg.Add(1)
		go func(name string, msgConfig struct {
			CycleTime  int    `yaml:"cycleTime"`
			SendId     uint32 `yaml:"sendId"`
			SendLength uint8  `yaml:"sendLength"`
			SendValue  []byte `yaml:"sendValue"`
			DesCan     string `yaml:"desCan"`
		}) {
			defer wg.Done()
			var output socketcan.Interface
			if msgConfig.DesCan == "can1" {
				output = can1
			} else if msgConfig.DesCan == "can2" {
				output = can2
			} else {
				log.Fatalf("Unknown CAN interface: %s", msgConfig.DesCan)
			}

			for {

				if atomic.LoadInt32(&cycleRunning) == 1 {
					msg := socketcan.CanFrame{
						ArbId:    msgConfig.SendId,
						Dlc:      msgConfig.SendLength,
						Data:     msgConfig.SendValue,
						Extended: true,
						IsFD:     true,
					}
					if err := output.SendFrame(msg); err != nil {
						log.Printf("Error transmitting frame %s: %v", name, err)
					} else {
						log.Printf("Sent frame %s with ID %d ", name, msg.ArbId)
					}
					time.Sleep(time.Duration(msgConfig.CycleTime) * time.Millisecond)
				} else {
					time.Sleep(200 * time.Millisecond) // 避免忙等待
				}
			}
		}(name, msgConfig)
	}
	wg.Wait()
}

func listenForControlMessage() {
	sub := subscribe(config.Listen.SourceCan)

	timer := time.NewTimer(200 * time.Millisecond)
	timer.Stop()

	go func() {
		for {
			frame := <-sub
			// 检查是否是 0x10FB04D8 报文
			if frame.ArbId == config.Listen.Id {
				log.Println("Received 0x10FB04D8, resetting timer and continuing cycleCan")
				timer.Reset(200 * time.Millisecond)
				atomic.StoreInt32(&cycleRunning, 1) // 设置 cycleRunning 为 true
			}
		}
	}()
	// 监听定时器
	go func() {
		for {
			<-timer.C
			log.Println("200ms timeout, stopping cycleCan")
			atomic.StoreInt32(&cycleRunning, 0) // 设置 cycleRunning 为 false
		}
	}()
	select {}
}

func sendOtaRequest(req OtaRequest) error {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	resp, err := http.Post("http://"+config.URL, "application/json", bytes.NewBuffer(reqJSON))
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status code: %d", resp.StatusCode)
	}

	return nil
}

func otaCanToCan() {
	sub := subscribe(config.OTA.SourceCan)

	// 一直收报文
	log.Println("OTA Listening on", config.OTA.SourceCan)
	go func() {
		for {
			frame := <-sub
			// 检查是否是 OTA 报文
			if frame.ArbId == config.OTA.Id {
				data := frame.Data[0]
				if data == config.OTA.Status1 {
					log.Printf("收到 OTA 信号 %02X", data)
					req := OtaRequest{ID: frame.ArbId, Data: data}
					if err := sendOtaRequest(req); err != nil {
						log.Printf("Error sending OTA request: %v", err)
					}
				} else if data == config.OTA.Status2 {
					log.Printf("收到 OTA 信号 %02X", data)
					req := OtaRequest{ID: frame.ArbId, Data: data}
					if err := sendOtaRequest(req); err != nil {
						log.Printf("Error sending OTA request: %v", err)
					}
				}
			}
		}
	}()
	select {}
}

func main() {
	// 加载配置文件
	if err := loadConfig("/data/configa62.yaml"); err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	// 初始化 CAN 接口
	if err := initCanInterfaces(); err != nil {
		log.Fatalf("初始化 CAN 接口失败: %v", err)
	}

	// 启动接收 Goroutine
	go startReceiver(can1, &can1Subscribers)
	go startReceiver(can2, &can2Subscribers)

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

	// 启动监听控制报文的任务
	go listenForControlMessage()

	// 启动周期性发送任务
	go cycleCan()

	//启动OTA转发
	go otaCanToCan()

	// 防止主 goroutine 退出
	select {}
}
