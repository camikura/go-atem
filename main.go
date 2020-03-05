package main

import (
	"flag"
	"fmt"
	"github.com/camikura/go-atem/atem"
)

var (
	ip    = flag.String("ip", "", "")
	debug = flag.Bool("debug", false, "")
)

func main() {
	flag.Parse()

	device := atem.NewDevice(*ip, 9910, *debug)
	device.On("connect", connected)
	device.Connect()
}

func connected() {
	fmt.Println("connected.")
}
