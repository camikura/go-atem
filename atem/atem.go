package atem

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"
)

type Callback func()

type Device struct {
	address   string
	conn      net.Conn
	callbacks map[string][]Callback
	state     uint16
	sid       uint16
	lpid      uint16
	debug     bool

	inPacket  chan []byte
	outPacket chan []byte
}

const (
	stateClosed      = 0x01
	stateSynsent     = 0x02
	stateEstablished = 0x03
)

const (
	flagSync    = 0x01
	flagConnect = 0x02
	flagRepeat  = 0x04
	flagError   = 0x08
	flagAck     = 0x16
)

type packet []byte

var helloPacket = []byte{0x10, 0x14, 0x0e, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0x26, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
var answerPacket = []byte{0x80, 0x0c, 0x0e, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0xfd, 0x00, 0x00}

func NewDevice(ip string, port int, debug bool) *Device {
	address := fmt.Sprintf("%s:%d", ip, port)
	callbacks := map[string][]Callback{}

	return &Device{address: address, state: stateClosed, callbacks: callbacks, debug: debug}
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
	conn, err := net.Dial("udp", d.address)
	if err != nil {
		log.Fatalln(err)
		return err
	}

	d.conn = conn
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
			b := make([]byte, 2060)
			//d.conn.SetReadDeadline(time.Now().Add(time.Second))
			l, _ := d.conn.Read(b)
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
		d.sendPacketAnswer()
	}

	if flag&flagSync > 0 {
		d.readSid(p)
		d.sendPacketPong(p)
		if len(p) > 12 {
			d.parseCommand(p[12:])
		}
		if len(p) == 12 && d.state == stateSynsent {
			d.state = stateEstablished
			fmt.Println("connected!!!")
		}
	}
}

func (d *Device) parseCommand(p []byte) {
	fmt.Println(len(p))
	dlen := binary.BigEndian.Uint16(p[0:2])
	name := string(p[4:8])
	d.readCommand(name, p[8:dlen])

	if len(p) > int(dlen) {
		d.parseCommand(p[dlen:])
	}
}

func (d *Device) readCommand(n string, p []byte) {
	switch n {
	case "PrgI":
		me := p[0]
		input := binary.BigEndian.Uint16(p[2:4])
		fmt.Printf("PrgI: %d %d\n", me, input)
	case "PrvI":
		me := p[0]
		input := binary.BigEndian.Uint16(p[2:4])
		fmt.Printf("PrvI: %d %d\n", me, input)
	}
}

func (d *Device) readSid(p []byte) {
	sid := binary.BigEndian.Uint16(p[2:4])

	if d.sid != 0 && d.sid != sid {
		return
	}

	log.Println(p[2:4], " ", d.sid)

	d.sid = sid
}

// send packet
func (d *Device) sendPacket(p []byte) {
	d.conn.Write(p)
	if d.debug {
		log.Println("Sent: ", p)
	}
}

func (d *Device) sendPacketHello() {
	d.sendPacket(helloPacket)
	//d.outPacket <- helloPacket
	d.state = stateSynsent
}

func (d *Device) sendPacketAnswer() {
	d.sendPacket(answerPacket)
	//d.outPacket <- answerPacket
}

func (d *Device) sendPacketPong(p []byte) {
	data := make([]byte, 12)

	data[0] = 128
	data[1] = 12
	data[2] = p[2]
	data[3] = p[3]
	data[4] = p[10]
	data[5] = p[11]

	d.sendPacket(data)
	//d.outPacket <- data
}
