package main

import (
	"log"
	"net"
	"time"
)

type udpWrite struct {
	addr *net.UDPAddr
	buf  []byte
}

type serverUdpListener struct {
	p     *program
	nconn *net.UDPConn
	flow  trackFlow
	mtu   int
	write chan *udpWrite
	done  chan struct{}
}

// GetLocalMTU returns the minimum MTU (hacky)
func getLocalMTU() int {
	mtu := 1500
	netInterfaces, err := net.Interfaces()
	if err == nil {
		for _, netInterface := range netInterfaces {
			if netInterface.MTU < mtu {
				mtu = netInterface.MTU
			}
		}
	}
	return mtu
}

func newServerUdpListener(p *program, port int, flow trackFlow) (*serverUdpListener, error) {
	nconn, err := net.ListenUDP("udp", &net.UDPAddr{
		Port: port,
	})
	if err != nil {
		return nil, err
	}

	l := &serverUdpListener{
		p:     p,
		nconn: nconn,
		flow:  flow,
		write: make(chan *udpWrite),
		done:  make(chan struct{}),
		mtu:   getLocalMTU(),
	}

	l.log("opened on :%d with MTU %d", port, l.mtu)
	return l, nil
}

func (l *serverUdpListener) log(format string, args ...interface{}) {
	var label string
	if l.flow == _TRACK_FLOW_RTP {
		label = "RTP"
	} else {
		label = "RTCP"
	}
	log.Printf("[UDP/"+label+" listener] "+format, args...)
}

func (l *serverUdpListener) run() {
	go func() {
		for w := range l.write {
			l.nconn.SetWriteDeadline(time.Now().Add(l.p.writeTimeout))
			l.nconn.WriteTo(w.buf, w.addr)
		}
	}()

	buf := make([]byte, 2048) // UDP MTU is 1400

	for {
		_, _, err := l.nconn.ReadFromUDP(buf)
		if err != nil {
			break
		}
	}

	close(l.write)

	close(l.done)
}

func (l *serverUdpListener) close() {
	l.nconn.Close()
	<-l.done
}
