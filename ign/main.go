package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/socketcan"
	"gopkg.in/yaml.v2"
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
}

var config Config = Config{}

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

func main() {

	dataBytes, err := os.ReadFile("/data/configig.yaml")
	if err != nil {
		fmt.Println("read config file failed", err)
		return
	}
	err = yaml.Unmarshal(dataBytes, &config)
	if err != nil {
		fmt.Println("解析 yaml 文件失败：", err)
		return
	}

	// 将config中的CycleMsg周期发送
	go cycleCan("can2")

	// 防止主 goroutine 退出
	select {}
}

//收报文
// func main() {

// 	inputConn, err := socketcan.DialContext(context.Background(), "can", "can2")
// 	if err != nil {
// 		log.Fatalf("Failed to open %s: %v", "can2", err)
// 	}
// 	defer inputConn.Close()

// 	// receive
// 	log.Println("Listening for messages on", "can2")
// 	go func() {
// 		inputRecv := socketcan.NewReceiver(inputConn)
// 		for inputRecv.Receive() {
// 			receiveMsg := inputRecv.Frame()

// 			log.Printf("%v\n", receiveMsg)
// 		}
// 	}()

// 	select {}
// }
