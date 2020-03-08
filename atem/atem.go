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

	knownPacket []uint16

	inPacket  chan []byte
	outPacket chan []byte

	// promotional
	Topology             Topology
	ProductId            string
	ProtocolVersionMajor int
	ProtocolVersionMinor int
	Warning              string
	ProgramInput         ProgramInput
	PreviewInput         PreviewInput
	Transition           []Transition
	TransitionPosition   []TransitionPosition
	DownstreamKeyer      []DownstreamKeyer
	AuxProperty          []Source
	AuxSource            []Source

	InputProperty map[int]Source
	MacroProperty map[int]Macro
	TallyByIndex  map[int]Tally
	TallyBySource map[int]Tally

	// callback
	OnConnected func(d *Device)
	OnClosed    func(d *Device)

	OnReceivedWarning func(d *Device, message string)
	OnReceivedCommand func(d *Device, command string, data []byte)

	OnChangedInputProperty      func(d *Device, id int, source Source)
	OnChangedMacroProperty      func(d *Device, id int, macro Macro)
	OnChangedMacroRunStatus     func(d *Device, id int, macro Macro, macroRunStatus MacroRunStatus)
	OnChangedProgramInput       func(d *Device, me int, id int, source Source)
	OnChangedPreviewInput       func(d *Device, me int, id int, source Source)
	OnChangedAuxSource          func(d *Device, index int, source Source, auxProperty AuxProperty)
	OnChangedTransition         func(d *Device, me int, transition Transition)
	OnChangedTransitionPosition func(d *Device, me int, transitionPosition TransitionPosition)
	OnChangedDownstreamKeyer    func(d *Device, index int, downstreamKeyer DownstreamKeyer)
	OnChangedTallyByIndex       func(d *Device, tallyByIndex TallyByIndex)
	OnChangedTallyBySource      func(d *Device, tallyBySource TallyBySource)
}

type Topology struct {
	Mes, Sources, ColorGenerators, Auxs, DownstreamKeyers, Stingers, Dves, Supersources int
}

type Source struct {
	Id, PortType        int
	Longname, Shortname string
}

type Tally struct{ Program, Preview bool }

type Macro struct {
	IsUsed            bool
	Name, Description string
}

type MacroRunStatus struct{ IsRunning, IsWaiting, IsLooping bool }

type Transition struct{ Style int }

type TransitionPosition struct {
	FrameRemaining, Position int
	InTransition             bool
}

type DownstreamKeyer struct {
	OnAir, InTransition, IsAutoTransitioning bool
	FrameRemaining                           int
}

type ProgramInput []Source
type PreviewInput []Source

type TallyByIndex map[int]Tally
type TallyBySource map[int]Tally

type AuxProperty Source

var (
	startPacket = []byte{0x10, 0x14, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x26, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	ackPacket   = []byte{0x80, 0x0c, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
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
	d.MacroProperty = make(map[int]Macro)
	d.TallyByIndex = make(map[int]Tally)
	d.TallyBySource = make(map[int]Tally)

	d.registerCallback()

	return &d
}

func (d *Device) registerCallback() {
	d.OnConnected = func(d *Device) {}
	d.OnClosed = func(d *Device) {}

	d.OnReceivedWarning = func(d *Device, message string) {}
	d.OnReceivedCommand = func(d *Device, command string, data []byte) {}
	d.OnChangedInputProperty = func(d *Device, id int, source Source) {}
	d.OnChangedMacroProperty = func(d *Device, id int, macro Macro) {}
	d.OnChangedMacroRunStatus = func(d *Device, id int, macro Macro, macrRunStatus MacroRunStatus) {}
	d.OnChangedProgramInput = func(d *Device, me int, id int, source Source) {}
	d.OnChangedPreviewInput = func(d *Device, me int, id int, source Source) {}
	d.OnChangedAuxSource = func(d *Device, index int, source Source, auxProperty AuxProperty) {}
	d.OnChangedTransition = func(d *Device, me int, transition Transition) {}
	d.OnChangedTransitionPosition = func(d *Device, me int, transitionPosition TransitionPosition) {}
	d.OnChangedDownstreamKeyer = func(d *Device, index int, downstreamKeyer DownstreamKeyer) {}
	d.OnChangedTallyByIndex = func(d *Device, tallyByIndex TallyByIndex) {}
	d.OnChangedTallyBySource = func(d *Device, tallyBySource TallyBySource) {}
}

func (d *Device) Connect() {
	err := d.connect()
	if err != nil {
		time.Sleep(time.Second * 3)
		d.Connect()
	}
}

func (d *Device) Close() {
	d.ConnState = ConnStateClosed
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

	d.sendPacketStart() // start communication
	d.waitPacket()      // main loop

	return nil
}

func (d *Device) IsClosed() bool     { return d.ConnState == ConnStateClosed }
func (d *Device) IsConnecting() bool { return d.ConnState == ConnStateConnecting }
func (d *Device) IsConnected() bool  { return d.ConnState == ConnStateConnected }

func (d *Device) waitPacket() {
	defer func() {
		d.OnClosed(d)
		d.sessionID = 0
		d.lastLocalPacketID = 0
	}()

	for !d.IsClosed() {
		b := make([]byte, 4096)
		d.conn.SetReadDeadline(time.Now().Add(time.Second))
		if l, _ := d.conn.Read(b); l > 0 {
			d.inPacket <- b[:l]
		}
	}
}

func (d *Device) recvPacket() {
	for {
		p := <-d.inPacket

		flag := int(p[0] >> 3)

		// hello answer
		if flag&flagHelloPacket > 0 {
			d.sendPacketAck(0)
		}

		// connected
		if flag&flagAck > 0 && d.IsConnecting() {
			d.ConnState = ConnStateConnected
			d.OnConnected(d)
		}

		if flag&flagAckRequest > 0 && !d.IsClosed() {
			d.sessionID = binary.BigEndian.Uint16(p[2:4]) // session id
			r := binary.BigEndian.Uint16(p[10:12])        // remote packet id

			// resend
			d.recordKnownPacket(r)
			if flag&flagResend > 0 && d.isKnownPacket(r) {
				continue
			}

			// ack response
			d.sendPacketAck(r)

			// receive commands
			if len(p) > 12 {
				d.parseCommand(p[12:])
			}
		}
	}
}

func (d *Device) isKnownPacket(r uint16) bool {
	for _, k := range d.knownPacket {
		if k == r {
			return true
		}
	}
	return false
}

func (d *Device) recordKnownPacket(r uint16) {
	if d.knownPacket = append(d.knownPacket, r); len(d.knownPacket) > 20 {
		d.knownPacket = d.knownPacket[1:]
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

	case "_ver":
		d.ProtocolVersionMajor = int(binary.BigEndian.Uint16(p[0:2]))
		d.ProtocolVersionMinor = int(binary.BigEndian.Uint16(p[2:4]))

	case "_pin":
		d.ProductId = d.createStringFromByte(p[:44])

	case "Warn":
		m := d.createStringFromByte(p[:44])
		d.Warning = m
		d.OnReceivedWarning(d, m)

	case "_top":
		d.Topology.Mes = int(p[0])
		d.Topology.Sources = int(p[1])
		d.Topology.ColorGenerators = int(p[2])
		d.Topology.Auxs = int(p[3])
		d.Topology.DownstreamKeyers = int(p[4] | 0x02)
		d.Topology.Stingers = int(p[5])
		d.Topology.Dves = int(p[6])
		d.Topology.Supersources = int(p[7])

		d.ProgramInput = make([]Source, d.Topology.Mes)
		d.PreviewInput = make([]Source, d.Topology.Mes)
		d.AuxProperty = make([]Source, d.Topology.Auxs)
		d.AuxSource = make([]Source, d.Topology.Auxs)
		d.Transition = make([]Transition, d.Topology.Mes)
		d.TransitionPosition = make([]TransitionPosition, d.Topology.Mes)

		d.DownstreamKeyer = make([]DownstreamKeyer, d.Topology.DownstreamKeyers)

	case "InPr":
		i := int(binary.BigEndian.Uint16(p[0:2])) // input
		ln := d.createStringFromByte(p[2:22])     // longname
		sn := d.createStringFromByte(p[22:26])    // shortname
		t := int(p[32])                           // port type
		s := Source{Id: i, PortType: t, Longname: ln, Shortname: sn}
		d.InputProperty[i] = s
		if s.PortType == 129 {
			d.AuxProperty[i-8001] = s
		}
		d.OnChangedInputProperty(d, i, s)

	case "MPrp":
		i := int(p[1])                                 // id
		u := p[2]&0x01 > 0                             // is used
		ln := int(binary.BigEndian.Uint16(p[4:6]))     // length of name
		le := int(binary.BigEndian.Uint16(p[6:8]))     // length of description
		n := d.createStringFromByte(p[8 : 8+ln])       // name
		e := d.createStringFromByte(p[8+ln : 8+ln+le]) // description
		m := Macro{IsUsed: u, Name: n, Description: e}
		d.MacroProperty[i] = m
		d.OnChangedMacroProperty(d, i, m)

	case "MRPr":
		r := p[0]&0x01 > 0                        // is running
		w := p[0]&0x02 > 0                        // is waiting
		l := p[1]&0x01 > 0                        // is looping
		i := int(binary.BigEndian.Uint16(p[2:4])) // id
		s := MacroRunStatus{IsRunning: r, IsWaiting: w, IsLooping: l}
		m := d.MacroProperty[i]
		d.OnChangedMacroRunStatus(d, i, m, s)

	case "PrgI":
		m := int(p[0])                            // me
		i := int(binary.BigEndian.Uint16(p[2:4])) // input
		s := d.InputProperty[i]
		d.ProgramInput[m] = s
		d.OnChangedProgramInput(d, m, i, s)

	case "PrvI":
		m := int(p[0])                            // me
		i := int(binary.BigEndian.Uint16(p[2:4])) // input
		s := d.InputProperty[i]
		d.PreviewInput[m] = s
		d.OnChangedPreviewInput(d, m, i, s)

	case "TrSS":
		m := int(p[0]) // me
		s := int(p[1]) // style
		t := Transition{Style: s}
		d.Transition[m] = t
		d.OnChangedTransition(d, m, t)

	case "TrPs":
		m := int(p[0])                            // me
		i := p[1]&0x01 > 0                        // in transition
		r := int(p[2])                            // frame remaining
		p := int(binary.BigEndian.Uint16(p[4:6])) // position
		t := TransitionPosition{InTransition: i, FrameRemaining: r, Position: p}
		d.TransitionPosition[m] = t
		d.OnChangedTransitionPosition(d, m, t)

	case "DskS":
		k := int(p[0])     // index
		o := p[1]&0x01 > 0 // onair
		i := p[2]&0x01 > 0 // in transition
		a := p[3]&0x01 > 0 // is auto transitioning
		r := int(p[4])     // frame remaining
		dsk := DownstreamKeyer{OnAir: o, InTransition: i, IsAutoTransitioning: a, FrameRemaining: r}
		d.DownstreamKeyer[k] = dsk
		d.OnChangedDownstreamKeyer(d, k, dsk)

	case "AuxS":
		i := int(p[0]) // index
		c := int(binary.BigEndian.Uint16(p[2:4]))
		s := d.InputProperty[c]
		a := d.AuxProperty[i]
		d.AuxSource[i] = s
		d.OnChangedAuxSource(d, i, s, AuxProperty(a))

	case "TlIn":
		l := int(binary.BigEndian.Uint16(p[0:2]))
		for i := 0; i < l; i++ {
			d.TallyByIndex[i] = Tally{
				Program: p[2+i]&0x01 > 0,
				Preview: p[2+i]&0x02 > 0,
			}
		}
		d.OnChangedTallyByIndex(d, d.TallyByIndex)

	case "TlSr":
		l := int(binary.BigEndian.Uint16(p[0:2]))
		for i := 0; i < l; i++ {
			k := int(binary.BigEndian.Uint16(p[2+i*3 : 4+i*3]))
			d.TallyBySource[k] = Tally{
				Program: p[4+i*3]&0x01 > 0,
				Preview: p[4+i*3]&0x02 > 0,
			}
		}
		d.OnChangedTallyBySource(d, d.TallyBySource)

	}
}

// exec command
func (d *Device) Cut(me int) {
	d.SendCommand("DCut", []byte{byte(me), 0, 0, 0})
}

func (d *Device) Auto(me int) {
	d.SendCommand("DAut", []byte{byte(me), 0, 0, 0})
}

func (d *Device) ChangeProgramInput(me int, source int) {
	d.SendCommand("CPgI", []byte{byte(me), 0, byte(source >> 0x08), byte(source & 0xff)})
}

func (d *Device) ChangePreviewInput(me int, source int) {
	d.SendCommand("CPvI", []byte{byte(me), 0, byte(source >> 0x08), byte(source & 0xff)})
}

func (d *Device) ChangeTransition(me int, style int, nextTransition []bool) {
	m := d.createBits([]bool{style >= 0, len(nextTransition) > 0})
	b := d.createBits(nextTransition)
	d.SendCommand("CTTp", []byte{m, byte(me), byte(style), b})
}

func (d *Device) ChangeTransitionPosition(me int, position int) {
	d.SendCommand("CTPs", []byte{byte(me), 0, byte(position >> 0x08), byte(position & 0xff)})
}

func (d *Device) ChangeKeyerOnAir(me int, keyer int, onair bool) {
	b := 0
	if onair {
		b |= 1
	}
	d.SendCommand("CKOn", []byte{byte(me), byte(keyer), byte(b), 0})
}

func (d *Device) ChangeAuxSource(index int, source int) {
	d.SendCommand("CAuS", []byte{0x01, byte(index), byte(source >> 0x08), byte(source & 0xff)})
}

func (d *Device) DownstreamKeyerAuto(keyer int) {
	d.SendCommand("DDsA", []byte{byte(keyer), 0x00, 0x00, 0x00})
}

func (d *Device) ChangeDownstreamKeyerTie(keyer int, tie bool) {
	b := 0
	if tie {
		b |= 1
	}
	d.SendCommand("CDsT", []byte{byte(keyer), byte(b), 0x00, 0x00})
}

func (d *Device) ChangeDownstreamKeyerRate(keyer int, rate int) {
	d.SendCommand("CDsR", []byte{byte(keyer), byte(rate), 0x00, 0x00})
}

func (d *Device) ChangeDownstreamKeyerOnAir(keyer int, onair bool) {
	b := 0
	if onair {
		b |= 1
	}
	d.SendCommand("CDsL", []byte{byte(keyer), byte(b), 0x00, 0x00})
}

func (d *Device) ChangeSuperSourceBoxParameters(b []byte) {
	d.SendCommand("CSBP", b)
}

func (d *Device) ChangeSuperSourceBoxParametersEnabled(box int, enabled bool) {
	b := make([]byte, 24)
	b[0] = byte(0x01)
	if enabled {
		b[1] |= 1
	}
	d.ChangeSuperSourceBoxParameters(b)
}

func (d *Device) ChangeSuperSourceBoxParametersSource(box int, source int) {
	b := make([]byte, 24)
	b[0] = byte(0x02)
	b[4] = byte(source >> 0x08)
	b[5] = byte(source & 0xff)
	d.ChangeSuperSourceBoxParameters(b)
}

func (d *Device) createBits(f []bool) byte {
	bits := byte(0)
	for i, b := range f {
		if b {
			bits |= 1 << (len(f) - i - 1)
		}
	}
	return bits
}

// send packet
func (d *Device) sendPacket() {
	for {
		p := <-d.outPacket

		p[2] = uint8(d.sessionID >> 0x08)
		p[3] = uint8(d.sessionID & 0xff)

		d.conn.Write(p)
		d.debug(fmt.Sprintf(">> %v", p))
	}
}

func (d *Device) SendCommand(n string, p []byte) {
	if !d.IsConnected() {
		return
	}

	d.lastLocalPacketID += 1

	l := 20 + len(p)
	b := make([]byte, l)

	b[0] = uint8(l>>0x08 | 0x08)
	b[1] = uint8(l & 0xff)
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

func (d *Device) sendPacketAck(r uint16) {
	b := ackPacket

	// set remote packet id
	b[4] = uint8(r >> 0x08)
	b[5] = uint8(r & 0xff)

	d.outPacket <- b
}

// tools
func (d *Device) SayConnectedMessage() {
	log.Printf("connected to \"%s\", protocol version is %d.%d\n", d.ProductId, d.ProtocolVersionMajor, d.ProtocolVersionMinor)
}

func (d *Device) SayClosedMessage() {
	log.Printf("disconnected to \"%s\"\n", d.ProductId)
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
