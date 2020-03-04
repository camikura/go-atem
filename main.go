package main

import (
	"flag"
	"fmt"
	"github.com/camikura/go-atem/atem"
)

var (
	debug = flag.Bool("debug", false, "Connection debugging")
)

func main() {
	flag.Parse()

	device := atem.NewDevice("192.168.10.242", 9910, *debug)
	device.On("connect", connected)
	device.Connect()
}

func connected() {
	fmt.Println("connected.")
}
