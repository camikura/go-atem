package atem

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"
)

type Device struct {
	conn net.Conn

	Ip        string
	Port      int
	ConnState uint16
	Debugmode bool

	sessionID         uint16
	lastLocalPacketID uint16

	inPacket  chan []byte
	outPacket chan []byte

	// promotional
	Topology           Topology
	ProductId          string
	ProgramInput       ProgramInput
	PreviewInput       PreviewInput
	Transition         []Transition
	TransitionPosition []TransitionPosition

	InputProperty map[int]Source

	OnConnected                 func(d *Device)
	OnReceivedCommand           func(d *Device, command string, data []byte)
	OnChangedInputProperty      func(d *Device, source Source)
	OnChangedProgramInput       func(d *Device, me int, source Source)
	OnChangedPreviewInput       func(d *Device, me int, source Source)
	OnChangedTransition         func(d *Device, me int, transition Transition)
	OnChangedTransitionPosition func(d *Device, me int, transitionPosition TransitionPosition)
}

type Topology struct {
	Mes, Sources, Colors, Auxs, Dsks, Stingers, Dves, Supersources int
}

type Source struct {
	Id                  int
	Longname, Shortname string
}

type Transition struct {
	Style int
}

type TransitionPosition struct {
	FrameRemaining int
	InTransition   bool
	Position       float32
}

type ProgramInput []Source
type PreviewInput []Source

var (
	startPacket       = []byte{0x10, 0x14, 0x0e, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0x26, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	helloAnswerPacket = []byte{0x80, 0x0c, 0x0e, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0xfd, 0x00, 0x00}
)

const (
	ConnStateClosed     = 0x01
	ConnStateConnecting = 0x02
	ConnStateConnected  = 0x03
)

const (
	flagAckRequest       = 0x01
	flagHelloPacket      = 0x02
	flagResend           = 0x04
	flagRequestNextAfter = 0x08
	flagAck              = 0x10
)

func NewDevice(ip string, port int, debugmode bool) *Device {
	d := Device{
		Ip:        ip,
		Port:      port,
		ConnState: ConnStateClosed,
		Debugmode: debugmode,
	}

	d.InputProperty = make(map[int]Source)

	d.OnConnected = func(d *Device) {}
	d.OnReceivedCommand = func(d *Device, command string, data []byte) {}
	d.OnChangedInputProperty = func(d *Device, source Source) {}
	d.OnChangedProgramInput = func(d *Device, me int, source Source) {}
	d.OnChangedPreviewInput = func(d *Device, me int, source Source) {}
	d.OnChangedTransition = func(d *Device, me int, transition Transition) {}
	d.OnChangedTransitionPosition = func(d *Device, me int, transitionPosition TransitionPosition) {}

	return &d
}

func (d *Device) Connect() {
	err := d.connect()
	if err != nil {
		time.Sleep(time.Second * 3)
		d.Connect()
	}
}

func (d *Device) connect() error {
	addr := fmt.Sprintf("%s:%d", d.Ip, d.Port)
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return err
	}

	d.conn = conn

	defer d.conn.Close()

	d.inPacket = make(chan []byte)
	d.outPacket = make(chan []byte)

	go d.sendPacket()
	go d.recvPacket()

	d.sendPacketStart()
	d.waitPacket() // main loop

	return nil
}

func (d *Device) waitPacket() {
	f := make(chan bool)

	go func(f chan bool) {
		for {
			b := make([]byte, 4096)
			d.conn.SetReadDeadline(time.Now().Add(time.Second))
			l, _ := d.conn.Read(b)
			if l > 0 {
				d.inPacket <- b[:l]
			}
		}
	}(f)

	<-f
}

func (d *Device) recvPacket() {
	for {
		p := <-d.inPacket

		flag := int(p[0] >> 3)

		if flag&flagHelloPacket > 0 {
			d.sendPacketHelloAnswer()
		}

		// connected
		if flag&flagAck > 0 {
			if d.ConnState != ConnStateConnected {
				d.ConnState = ConnStateConnected
				d.OnConnected(d)
			}
		}

		if flag&flagAckRequest > 0 {
			d.sessionID = binary.BigEndian.Uint16(p[2:4])

			rpid := binary.BigEndian.Uint16(p[10:12])
			d.sendPacketAck(rpid)

			// receive commands
			if len(p) > 12 {
				d.parseCommand(p[12:])
			}
		}
	}
}

func (d *Device) parseCommand(p []byte) {
	m := binary.BigEndian.Uint16(p[0:2]) // length of command
	n := string(p[4:8])                  // name of command

	d.readCommand(n, p[8:m])
	d.OnReceivedCommand(d, n, p[8:m])

	// for multiple command
	if len(p) > int(m) {
		d.parseCommand(p[m:])
	}
}

func (d *Device) readCommand(n string, p []byte) {
	switch n {

	case "_pin":
		d.ProductId = d.createStringFromByte(p[:44])

	case "InPr":
		i := int(binary.BigEndian.Uint16(p[0:2]))
		ln := d.createStringFromByte(p[2:22])
		sn := d.createStringFromByte(p[22:26])
		s := Source{Id: i, Longname: ln, Shortname: sn}
		d.InputProperty[i] = s
		d.OnChangedInputProperty(d, s)

	case "_top":
		d.Topology.Mes = int(p[0])
		d.Topology.Sources = int(p[1])
		d.Topology.Colors = int(p[2])
		d.Topology.Auxs = int(p[3])
		d.Topology.Dsks = int(p[4])
		d.Topology.Stingers = int(p[5])
		d.Topology.Dves = int(p[6])
		d.Topology.Supersources = int(p[7])

		d.ProgramInput = make([]Source, d.Topology.Mes)
		d.PreviewInput = make([]Source, d.Topology.Mes)
		d.Transition = make([]Transition, d.Topology.Mes)
		d.TransitionPosition = make([]TransitionPosition, d.Topology.Mes)

	case "PrgI":
		m := int(p[0])
		i := int(binary.BigEndian.Uint16(p[2:4]))
		s := d.InputProperty[i]
		d.ProgramInput[m] = s
		d.OnChangedProgramInput(d, m, s)

	case "PrvI":
		m := int(p[0])
		i := int(binary.BigEndian.Uint16(p[2:4]))
		s := d.InputProperty[i]
		d.PreviewInput[m] = s
		d.OnChangedPreviewInput(d, m, s)

	case "TrSS":
		m := int(p[0])
		s := int(p[1])
		t := Transition{Style: s}
		d.Transition[m] = t
		d.OnChangedTransition(d, m, t)

	case "TrPs":
		m := int(p[0])
		i := p[1]&0x01 > 0
		r := int(p[2])
		p := float32(binary.BigEndian.Uint16(p[4:6])) * 0.0001
		t := TransitionPosition{InTransition: i, FrameRemaining: r, Position: p}
		d.TransitionPosition[m] = t
		d.OnChangedTransitionPosition(d, m, t)

	}
}

// send packet
func (d *Device) sendPacket() {
	for {
		p := <-d.outPacket

		d.conn.Write(p)
		d.debug(fmt.Sprintf(">> %v", p))
	}
}

func (d *Device) SendCommand(n string, p []byte) {
	d.lastLocalPacketID += 1

	l := 20 + len(p)
	b := make([]byte, l)

	b[0] = uint8(l>>0x08 | 0x08)
	b[1] = uint8(l & 0xff)
	b[2] = uint8(d.sessionID >> 0x08)
	b[3] = uint8(d.sessionID & 0xff)
	b[10] = uint8(d.lastLocalPacketID >> 0x08)
	b[11] = uint8(d.lastLocalPacketID & 0xff)
	b[12] = uint8((8 + len(n)) >> 0x08)
	b[13] = uint8((8 + len(n)) & 0xff)

	for i := 0; i < len(n); i++ {
		b[16+i] = n[i]
	}
	for i := 0; i < len(p); i++ {
		b[20+i] = p[i]
	}

	d.outPacket <- b
}

func (d *Device) sendPacketStart() {
	d.outPacket <- startPacket
	d.ConnState = ConnStateConnecting
}

func (d *Device) sendPacketHelloAnswer() {
	d.outPacket <- helloAnswerPacket
}

func (d *Device) sendPacketAck(r uint16) {
	b := make([]byte, 12)

	b[0] = 128
	b[1] = 12
	b[2] = uint8(d.sessionID >> 0x08)
	b[3] = uint8(d.sessionID & 0xff)
	b[4] = uint8(r >> 0x08)
	b[5] = uint8(r & 0xff)

	d.outPacket <- b
}

func (d *Device) createStringFromByte(b []byte) string {
	if l := bytes.IndexByte(b, 0); l >= 0 {
		return string(b[:l])
	}
	return string(b)
}

func (d *Device) debug(v interface{}) {
	if d.Debugmode {
		log.Println(v)
	}
}
