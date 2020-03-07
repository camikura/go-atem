package main

import (
	"flag"
	"github.com/camikura/go-atem/atem"
	"github.com/hypebeast/go-osc/osc"
)

var (
	device *atem.Device

	ip   = flag.String("ip", "", "IP Address")
	port = flag.Int("port", 9910, "Port Number")
)

func main() {
	flag.Parse()

	initAtem()
	initOSC()

	for {
	}
}

func initOSC() {
	a := "127.0.0.1:8765"
	d := osc.NewStandardDispatcher()
	d.AddMsgHandler("/auto", func(_ *osc.Message) { device.Auto(0) })
	d.AddMsgHandler("/cut", func(_ *osc.Message) { device.Cut(0) })
	server := &osc.Server{Addr: a, Dispatcher: d}
	go server.ListenAndServe()
}

func initAtem() {
	device = atem.NewDevice(*ip, *port, false)
	device.OnConnected = func(d *atem.Device) { d.SayConnectedMessage() }
	go device.Connect()
}
