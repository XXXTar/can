package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/socketcan"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ReceiveSend map[uint32]struct {
		Value      [can.MaxDataLength]byte `yaml:"Value"`
		SendId     uint32                  `yaml:"sendId"`
		SendLength uint8                   `yaml:"sendLength"`
		SendValue  [can.MaxDataLength]byte `yaml:"sendValue"`
	} `yaml:"receiveSend"`


}

func main() {
	// var start bool
	if len(os.Args) < 3 {
		fmt.Println("usage: ./can-specified-reponse <can intf> <config file>")
		return
	}

	canInft := os.Args[1]
	configFile := os.Args[2]
	ext := filepath.Ext(configFile)
	switch ext {
	case ".yaml", "yml":
		fmt.Println("load config ", configFile)
	default:
		fmt.Println("only support .yaml or .yml file")
		return
	}

	dataBytes, err := os.ReadFile(configFile)
	if err != nil {
		fmt.Println("read config file failed", err)
		return
	}

	config := Config{}

	err = yaml.Unmarshal(dataBytes, &config)
	if err != nil {
		fmt.Println("解析 yaml 文件失败：", err)
		return
	}

	// // 打印解析后的配置
	// fmt.Printf("ReceiveSend:\n")
	// for key, entry := range config.ReceiveSend {
	// 	fmt.Printf("Key: %s\n", key)
	// 	fmt.Printf("  ReceiveId: 0x%08X\n", entry.SendId)
	// 	fmt.Printf("  ReceiveValue: %v\n", entry.Value)
	// 	fmt.Printf("  SendId: 0x%08X\n", entry.SendId)
	// 	fmt.Printf("  SendValue: %v\n", entry.SendValue)
	// 	start = true
	// }
	// if start {
	// 	return
	// }

	conn, _ := socketcan.DialContext(context.Background(), "can", canInft)
	recv := socketcan.NewReceiver(conn)
	tx := socketcan.NewTransmitter(conn)

	log.Println("listenging messages on ", canInft)

	var wg sync.WaitGroup
	msgChan := make(chan can.Frame, 1000)

	// receive
	wg.Add(1)
	go func() {
		defer wg.Done()
		for recv.Receive() {
			receiveMsg := recv.Frame()
			if entry, exists := config.ReceiveSend[receiveMsg.ID]; exists {
				if receiveMsg.Data == config.ReceiveSend[receiveMsg.ID].Value {
					msgChan <- can.Frame{
						ID:         entry.SendId,
						Length:     entry.SendLength,
						Data:       entry.SendValue,
						IsExtended: true,
					}
				}
			}

		}
	}()

	// send
	wg.Add(1)
	go func() {
		defer wg.Done()
		for sendMsg := range msgChan {
			log.Println("send message: ", sendMsg.ID, sendMsg.Data)
			tx.TransmitFrame(context.Background(), sendMsg)
		}
	}()

	wg.Wait()

}
