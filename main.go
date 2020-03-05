package main

import (
	"flag"
	"github.com/camikura/go-atem/atem"
	"log"
)

var (
	ip    = flag.String("ip", "", "IP Address")
	port  = flag.Int("port", 9910, "Port Number")
	debug = flag.Bool("debug", false, "Bool flag for Debug mode")
)

var (
	device *atem.Device
)

func main() {
	flag.Parse()

	device = atem.NewDevice(*ip, *port, *debug)
	device.On("connected", connected)
	device.On("topologyChanged", topologyChanged)
	device.On("statusChanged", statusChanged)
	device.Connect()
}

func connected() {
	log.Println("conn: connected")
}

func topologyChanged() {
	log.Println("topo:", device.Topology)
}

func statusChanged() {
	log.Println("stats:", device.Status)
}
