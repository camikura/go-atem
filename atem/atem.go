package atem

import (
	_ "bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"reflect"
	"time"
)

type Callback func()

type Device struct {
	localAddr  net.Addr
	remoteAddr net.Addr
	conn       net.PacketConn
	callbacks  map[string][]Callback
	connState  uint16
	sid        uint16
	lpid       uint16
	debugmode  bool

	inPacket  chan []byte
	outPacket chan []byte

	Topology Topology
	Status   Status
}

const (
	connStateClosed      = 0x01
	connStateSynsent     = 0x02
	connStateEstablished = 0x03
)

const (
	flagSync    = 0x01
	flagConnect = 0x02
	flagRepeat  = 0x04
	flagError   = 0x08
	flagAck     = 0x16
)

type Topology struct {
	Mes, Sources, Colors, Auxs, Dsks, Stingers, Dves, Supersources int
}

type Status struct {
	Prgi, Prvi [4]int
}

var helloPacket = []byte{0x10, 0x14, 0x0e, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0x26, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
var answerPacket = []byte{0x80, 0x0c, 0x0e, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0xfd, 0x00, 0x00}

func NewDevice(ip string, port int, debugmode bool) *Device {
	addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, port))
	callbacks := map[string][]Callback{}
	return &Device{remoteAddr: addr, connState: connStateClosed, callbacks: callbacks, debugmode: debugmode}
}

func (d *Device) On(e string, cb func()) {
	_, f := d.callbacks[e]
	if !f {
		d.callbacks[e] = make([]Callback, 0)
	}
	d.callbacks[e] = append(d.callbacks[e], cb)
}

func (d *Device) handle(e string, p ...interface{}) {
	l, x := d.callbacks[e]
	if x {
		i := make([]reflect.Value, len(p))
		for k, r := range p {
			i[k] = reflect.ValueOf(r)
		}
		for _, cb := range l {
			reflect.ValueOf(cb).Call(i)
		}
	}
}

func (d *Device) Connect() {
	err := d.connect()
	if err != nil {
		time.Sleep(time.Second * 3)
		d.Connect()
	}
}

func (d *Device) connect() error {
	conn, err := net.Dial("udp", d.remoteAddr.String())
	if err != nil {
		return err
	}

	d.conn, err = net.ListenPacket("udp", conn.LocalAddr().String())
	if err != nil {
		return err
	}

	defer d.conn.Close()

	d.inPacket = make(chan []byte)
	d.outPacket = make(chan []byte)

	go d.sendPacket()
	go d.recvPacket()

	d.sendPacketHello()
	d.waitPacket()

	return nil
}

func (d *Device) waitPacket() {
	f := make(chan bool)

	go func(f chan bool) {
		for {
			b := make([]byte, 4096)
			d.conn.SetReadDeadline(time.Now().Add(time.Second * 3))
			l, _, _ := d.conn.ReadFrom(b)
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

		if flag&flagConnect > 0 && flag&flagRepeat == 0 {
			d.debug(fmt.Sprintf("Recv: %v", p))
			d.sendPacketAnswer()
		}

		if flag&flagSync > 0 {
			d.sendPacketPong(p)

			if len(p) == 12 && d.connState == connStateSynsent {
				d.sid = binary.BigEndian.Uint16(p[2:4])
				d.connState = connStateEstablished
				d.handle("connected")
			}

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

	// for multiple command
	if len(p) > int(m) {
		d.parseCommand(p[m:])
	}
}

func (d *Device) readCommand(n string, p []byte) {
	switch n {
	case "_top":
		d.Topology.Mes = int(p[0])
		d.Topology.Sources = int(p[1])
		d.Topology.Colors = int(p[2])
		d.Topology.Auxs = int(p[3])
		d.Topology.Dsks = int(p[4])
		d.Topology.Stingers = int(p[5])
		d.Topology.Dves = int(p[6])
		d.Topology.Supersources = int(p[7])
		d.debug(fmt.Sprintf("_top: %v", d.Topology))
		d.handle("topologyChanged")
	case "PrgI":
		me := int(p[0])
		d.Status.Prgi[me] = int(binary.BigEndian.Uint16(p[2:4]))
		d.debug(fmt.Sprintf("PrgI: %v", d.Status))
		d.handle("statusChanged")
	case "PrvI":
		me := int(p[0])
		d.Status.Prvi[me] = int(binary.BigEndian.Uint16(p[2:4]))
		d.debug(fmt.Sprintf("PrvI: %v", d.Status))
		d.handle("statusChanged")
	default:
		//log.Printf("%s: %v\n", n, p)
	}
}

// send packet
func (d *Device) sendPacket() {
	for {
		p := <-d.outPacket

		d.conn.WriteTo(p, d.remoteAddr)
		d.debug(fmt.Sprintf("Sent: %v", p))
	}
}

func (d *Device) SendCommand(n string, p []byte) {
	d.lpid += 1

	l := 20 + len(p)
	b := make([]byte, l)

	b[0] = uint8(l>>0x08 | 0x08)
	b[1] = uint8(l & 0xff)
	b[2] = uint8(d.sid >> 0x08)
	b[3] = uint8(d.sid & 0xff)
	b[10] = uint8(d.lpid >> 0x08)
	b[11] = uint8(d.lpid & 0xff)
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

func (d *Device) sendPacketHello() {
	d.outPacket <- helloPacket
	d.connState = connStateSynsent
}

func (d *Device) sendPacketAnswer() {
	d.outPacket <- answerPacket
}

func (d *Device) sendPacketPong(p []byte) {
	b := make([]byte, 12)

	b[0] = 128
	b[1] = 12
	b[2] = uint8(d.sid >> 0x08)
	b[3] = uint8(d.sid & 0xff)
	b[4] = p[10]
	b[5] = p[11]

	d.outPacket <- b
}

func (d *Device) debug(v interface{}) {
	if d.debugmode {
		log.Println(v)
	}
}
