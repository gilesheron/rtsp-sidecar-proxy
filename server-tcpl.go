package main

import (
	"log"
	"net"
	"sync"

	"github.com/aler9/gortsplib"
)

type serverTcpListener struct {
	p       *program
	listen  *net.TCPListener
	mutex   sync.RWMutex
	clients map[*serverClient]struct{}
	done    chan struct{}
}

func newServerTcpListener(p *program) (*serverTcpListener, error) {
	listen, err := net.ListenTCP("tcp", &net.TCPAddr{
		Port: p.conf.Server.RtspPort,
	})
	if err != nil {
		return nil, err
	}

	l := &serverTcpListener{
		p:       p,
		listen:   listen,
		clients: make(map[*serverClient]struct{}),
		done:    make(chan struct{}),
	}

	l.log("opened on :%d", p.conf.Server.RtspPort)
	return l, nil
}

func (l *serverTcpListener) log(format string, args ...interface{}) {
	log.Printf("[TCP listener] "+format, args...)
}

func (l *serverTcpListener) run() {
	for {
		l.log("listening for TCP connections")
		nconn, err := l.listen.AcceptTCP()
		if err != nil {
			l.log("Unable to accept TCP connection %s", err.Error())
			break
		}

		newServerClient(l.p, nconn)

		l.log("Started new server client %s", nconn.RemoteAddr().String())
	}

	l.log ("closing TCP clients")

	// close clients
	var doneChans []chan struct{}
	func() {
		l.mutex.Lock()
		defer l.mutex.Unlock()
		for c := range l.clients {
			c.close()
			doneChans = append(doneChans, c.done)
		}
	}()
	for _, c := range doneChans {
		<-c
	}

	close(l.done)
}

func (l *serverTcpListener) close() {
	l.listen.Close()
	<-l.done
}

func (l *serverTcpListener) forwardTrack(path string, id int, flow trackFlow, frame []byte) {
	for c := range l.clients {
		if c.path == path && c.state == _CLIENT_STATE_PLAY {
			if c.streamProtocol == _STREAM_PROTOCOL_UDP {
				if flow == _TRACK_FLOW_RTP {
					l.p.udplRtp.write <- &udpWrite{
						addr: &net.UDPAddr{
							IP:   c.ip(),
							Zone: c.zone(),
							Port: c.streamTracks[id].rtpPort,
						},
						buf: frame,
					}
					l.p.udplRtp.write <- &udpWrite{
						addr: &net.UDPAddr{
							IP:   c.ip(),
							Zone: c.zone(),
							Port: c.streamTracks[id].rtpPort,
						},
						buf: frame,
					}
				} else {
					l.p.udplRtcp.write <- &udpWrite{
						addr: &net.UDPAddr{
							IP:   c.ip(),
							Zone: c.zone(),
							Port: c.streamTracks[id].rtcpPort,
						},
						buf: frame,
					}
				}

			} else {
				c.write <- &gortsplib.InterleavedFrame{
					Channel: trackToInterleavedChannel(id, flow),
					Content: frame,
				}
			}
		}
	}
}
