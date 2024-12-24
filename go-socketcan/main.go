package main

import (
	"fmt"
	"os"
	"sync"

	"source-crdc.hirain.com/atx-go/go-socketcan/pkg/socketcan"
)

func main() {
	input, err := socketcan.NewRawInterface("can1")
	if err != nil {
		fmt.Printf("could not open interface %s: %v\n",
			"can1", err)
		os.Exit(1)
	}
	defer input.Close()

	output, err := socketcan.NewRawInterface("can2")
	if err != nil {
		fmt.Printf("could not open interface %s: %v\n",
			"can1", err)
		os.Exit(1)
	}
	defer input.Close()

	//frame := socketcan.CanFrame{}
	//device.SendFrame(frame)
	var wg sync.WaitGroup
	msgChan := make(chan socketcan.CanFrame, 1000)
	// send
	fmt.Println("Sending messages from", "can1", "to", "can1")
	wg.Add(1)
	go func() {
		defer wg.Done()
		for sendMsg := range msgChan {
			if err := output.SendFrame(sendMsg); err != nil {
				fmt.Printf("Error transmitting frame to %s: %v", "can1", err)
			}
		}
	}()

	// receive
	fmt.Println("Listening for messages on", "can1")
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			frame, err := input.RecvFrame()
			if err != nil {
				fmt.Printf("error receiving frame: %v", err)
				os.Exit(1)
			}
			frame.IsFD = true
			msgChan <- frame
		}
	}()

	wg.Wait()

}
