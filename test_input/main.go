package main

import (
	"context"
	"log"
	"sync"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/candevice"
	"go.einride.tech/can/pkg/socketcan"
)

func main() {
	var wg sync.WaitGroup
	msgChan := make(chan can.Frame, 1000)

	d,_ := candevice.New("")

	inputConn, err := socketcan.DialContext(context.Background(), "can", "can1")
	if err != nil {
		log.Fatalf("Failed to open %s: %v", "can1", err)
	}
	defer inputConn.Close()

	outputConn, err := socketcan.DialContext(context.Background(), "can", "can1")
	if err != nil {
		log.Fatalf("Failed to open %s: %v", "can1", err)
	}
	defer outputConn.Close()

	// send
	log.Println("Sending messages from", "can1", "to", "can1")
	wg.Add(1)
	go func() {
		defer wg.Done()
		for sendMsg := range msgChan {
			tx := socketcan.NewTransmitter(outputConn)
			if err := tx.TransmitFrame(context.Background(), sendMsg); err != nil {
				log.Printf("Error transmitting frame to %s: %v", "can1", err)
			}
		}
	}()

	// receive
	log.Println("Listening for messages on", "can1")
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputRecv := socketcan.NewReceiver(inputConn)
		for inputRecv.Receive() {
			receiveMsg := inputRecv.Frame()
			if receiveMsg.ID == 0x12345678 {
				log.Println(receiveMsg.Data)
				msgChan <- can.Frame{
					ID:         0x12345678,
					Length:     64,
					Data:       receiveMsg.Data,
					IsExtended: true,
				}
			}
		}
	}()

	wg.Wait()
}
