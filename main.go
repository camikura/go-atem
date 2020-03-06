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

	device.OnConnected = connected
	device.OnReceivedCommand = receivedCommand
	device.OnChangedProgramInput = changedProgramInput
	device.OnChangedPreviewInput = changedPreviewInput
	device.OnChangedTransition = changedTransition
	device.OnChangedTransitionPosition = changedTransitionPosition

	device.Connect()
}

func connected(d *atem.Device) {
	log.Println("connected to", d.ProductId)
}

// for debug
func receivedCommand(d *atem.Device, c string, p []byte) {
	//log.Println("got command", c, p)
}

func changedProgramInput(d *atem.Device, m int, s atem.Source) {
	log.Println("changed program input", m, s.Id, s.Longname, s.Shortname)
}

func changedPreviewInput(d *atem.Device, m int, s atem.Source) {
	log.Println("changed preview input", m, s.Id, s.Longname, s.Shortname)
}

func changedTransition(d *atem.Device, m int, t atem.Transition) {
	log.Println("change transition", m, t.Style)
}

func changedTransitionPosition(d *atem.Device, m int, t atem.TransitionPosition) {
	log.Println("change transition position", m, t.InTransition, t.FrameRemaining, t.Position)
}
