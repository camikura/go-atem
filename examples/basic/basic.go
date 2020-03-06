package main

import (
	"flag"
	"github.com/camikura/go-atem/atem"
	"log"
)

var (
	device *atem.Device

	ip    = flag.String("ip", "", "IP Address")
	port  = flag.Int("port", 9910, "Port Number")
	debug = flag.Bool("debug", false, "Bool flag for Debug mode")
)

func main() {
	flag.Parse()

	device = atem.NewDevice(*ip, *port, *debug)

	device.OnConnected = connected

	device.OnReceivedWarning = receivedWarning
	device.OnReceivedCommand = receivedCommand
	device.OnChangedInputProperty = changedInputProperty
	device.OnChangedMacroProperty = changedMacroProperty
	device.OnChangedMacroRunStatus = changedMacroRunStatus
	device.OnChangedProgramInput = changedProgramInput
	device.OnChangedPreviewInput = changedPreviewInput
	device.OnChangedTransition = changedTransition
	device.OnChangedTransitionPosition = changedTransitionPosition
	device.OnChangedDownstreamKeyer = changedDownstreamKeyer

	//device.OnChangedTallyByIndex = changedTallyByIndex
	//device.OnChangedTallyBySource = changedTallyBySource

	device.Connect()
}

func connected(d *atem.Device) {
	log.Printf("connected to \"%s\", protocol version is %d.%d", d.ProductId, d.ProtocolVersionMajor, d.ProtocolVersionMinor)
}

func receivedWarning(d *atem.Device, m string) {
	log.Println("got warning", m)
}

// for debug
func receivedCommand(d *atem.Device, c string, p []byte) {
	//log.Println("got command", c, p)
}

func changedInputProperty(d *atem.Device, i int, s atem.Source) {
	log.Println("changed input property", i, s.Longname, s.Shortname)
}

func changedMacroProperty(d *atem.Device, i int, m atem.Macro) {
	if m.IsUsed {
		log.Println("changed macro property", i, m.IsUsed, m.Name, m.Description)
	}
}

func changedMacroRunStatus(d *atem.Device, i int, m atem.Macro, s atem.MacroRunStatus) {
	log.Println("changed macro run status", i, m.Name, s.IsRunning, s.IsWaiting, s.IsLooping)
}

func changedProgramInput(d *atem.Device, m int, i int, s atem.Source) {
	log.Println("changed program input", m, i, s.Longname, s.Shortname)
}

func changedPreviewInput(d *atem.Device, m int, i int, s atem.Source) {
	log.Println("changed preview input", m, i, s.Longname, s.Shortname)
}

func changedTransition(d *atem.Device, m int, t atem.Transition) {
	log.Println("changed transition", m, t.Style)
}

func changedTransitionPosition(d *atem.Device, m int, t atem.TransitionPosition) {
	log.Println("changed transition position", m, t.InTransition, t.FrameRemaining, t.Position)
}

func changedDownstreamKeyer(d *atem.Device, i int, k atem.DownstreamKeyer) {
	log.Println("changed downstream keyer", i, k.OnAir, k.InTransition, k.FrameRemaining)
}

func changedTallyByIndex(d *atem.Device, t atem.TallyByIndex) {
	for i, l := range t {
		s := d.InputProperty[i]
		log.Println(l, s)
	}
	log.Println("changed tally by index", t)
}

func changedTallyBySource(d *atem.Device, t atem.TallyBySource) {
	for i, l := range t {
		s := d.InputProperty[i]
		log.Println(l, s)
	}
	log.Println("changed tally by source", t)
}