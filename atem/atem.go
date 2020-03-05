package atem

import (
	_ "bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
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
	debug      bool

	inPacket  chan []byte
	outPacket chan []byte

	topology Topology
	status   Status
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
	mes, sources, colors, auxs, dsks, stingers, dves, supersources int
}

type Status struct {
	prgi, prvi [4]int
}

var helloPacket = []byte{0x10, 0x14, 0x0e, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0x26, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
var answerPacket = []byte{0x80, 0x0c, 0x0e, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0xfd, 0x00, 0x00}

func NewDevice(ip string, port int, debug bool) *Device {
	addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, port))
	callbacks := map[string][]Callback{}
	return &Device{remoteAddr: addr, connState: connStateClosed, callbacks: callbacks, debug: debug}
}

func (d *Device) On(e string, cb func()) {
	_, f := d.callbacks[e]
	if !f {
		d.callbacks[e] = make([]Callback, 0)
	}
	d.callbacks[e] = append(d.callbacks[e], cb)
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

	fmt.Println(conn.LocalAddr().String())
	addr := conn.LocalAddr().String()
	d.conn, err = net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}

	defer d.conn.Close()

	//d.inPacket = make(chan []byte)
	//d.outPacket = make(chan []byte)

	//go d.sendPacket()
	//go d.recvPacket()

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
				d.recvPacket(b[:l])
				// d.inPacket <- b[:l]
			}
		}
	}(f)

	<-f
}

func (d *Device) recvPacket(p []byte) {
	flag := int(p[0] >> 3)

	if flag&flagConnect > 0 && flag&flagRepeat == 0 {
		log.Println("Recv:", p)
		d.sendPacketAnswer()
	}

	if flag&flagSync > 0 {
		if len(p) > 12 {
			d.parseCommand(p[12:], p[:12])
		}
		if len(p) == 12 {
			if d.connState == connStateSynsent {
				d.readSid(p)
				d.connState = connStateEstablished // connected
			} else {
				log.Println("Recv:", p)
				d.sendPacketPong(p)
			}
		}
	}
}

func (d *Device) parseCommand(p []byte, h []byte) {
	m := binary.BigEndian.Uint16(p[0:2]) // length of command
	n := string(p[4:8])                  // name of command

	d.readCommand(n, p[8:m], h)

	// for multiple command
	if len(p) > int(m) {
		d.parseCommand(p[m:], p[m:m+12])
	}
}

func (d *Device) readCommand(n string, p []byte, h []byte) {
	switch n {
	case "_top":
		d.topology.mes = int(p[0])
		d.topology.sources = int(p[1])
		d.topology.colors = int(p[2])
		d.topology.auxs = int(p[3])
		d.topology.dsks = int(p[4])
		d.topology.stingers = int(p[5])
		d.topology.dves = int(p[6])
		d.topology.supersources = int(p[7])
		log.Println("_top:", d.topology)
	case "PrgI":
		me := int(p[0])
		d.status.prgi[me] = int(binary.BigEndian.Uint16(p[2:4]))
		log.Println("PrgI:", d.status)
	case "PrvI":
		me := int(p[0])
		d.status.prvi[me] = int(binary.BigEndian.Uint16(p[2:4]))
		log.Println("PrvI:", d.status)
	default:
		//log.Printf("%s: %v\n", n, p)
	}
}

func (d *Device) readSid(p []byte) {
	sid := binary.BigEndian.Uint16(p[2:4])

	if d.sid != 0 && d.sid != sid {
		return
	}

	d.sid = sid
}

// send packet
func (d *Device) sendPacket(p []byte) {
	d.conn.WriteTo(p, d.remoteAddr)
	if d.debug {
		log.Println("Sent:", p)
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

	d.sendPacket(b)
}

func (d *Device) sendPacketHello() {
	d.sendPacket(helloPacket)
	//d.outPacket <- helloPacket
	d.connState = connStateSynsent
}

func (d *Device) sendPacketAnswer() {
	d.sendPacket(answerPacket)
	//d.outPacket <- answerPacket
}

func (d *Device) sendPacketPong(p []byte) {
	b := make([]byte, 12)

	b[0] = 128
	b[1] = 12
	b[2] = p[2]
	b[3] = p[3]
	b[4] = p[10]
	b[5] = p[11]

	//d.outPacket <- buf
	time.Sleep(time.Second)
	d.sendPacket(b)
}
