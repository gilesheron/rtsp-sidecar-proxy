package main

import (
	"errors"
	"fmt"
	"github.com/aler9/gortsplib"
	"gortc.io/sdp"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

func interleavedChannelToTrack(channel uint8) (int, trackFlow) {
	if (channel % 2) == 0 {
		return int(channel / 2), _TRACK_FLOW_RTP
	}
	return int((channel - 1) / 2), _TRACK_FLOW_RTCP
}

func trackToInterleavedChannel(id int, flow trackFlow) uint8 {
	if flow == _TRACK_FLOW_RTP {
		return uint8(id * 2)
	}
	return uint8((id * 2) + 1)
}

type clientState int

const (
	_CLIENT_STATE_STARTING clientState = iota
	_CLIENT_STATE_PRE_PLAY
	_CLIENT_STATE_PLAY
)

func (cs clientState) String() string {
	switch cs {
	case _CLIENT_STATE_STARTING:
		return "STARTING"

	case _CLIENT_STATE_PRE_PLAY:
		return "PRE_PLAY"

	case _CLIENT_STATE_PLAY:
		return "PLAY"
	}
	return "UNKNOWN"
}

type serverClient struct {
	p              *program
	conn           *gortsplib.ConnServer
	state          clientState
	path           string
	readAuth       *gortsplib.AuthServer
	streamProtocol streamProtocol
	streamTracks   []*track
	write          chan *gortsplib.InterleavedFrame
	done           chan struct{}
}

func newServerClient(p *program, nconn net.Conn) *serverClient {
	c := &serverClient{
		p: p,
		conn: gortsplib.NewConnServer(gortsplib.ConnServerConf{
			NConn:        nconn,
			ReadTimeout:  p.readTimeout,
			WriteTimeout: p.writeTimeout,
		}),
		state: _CLIENT_STATE_STARTING,
		path: "",
		write: make(chan *gortsplib.InterleavedFrame),
		done:  make(chan struct{}),
	}

	c.p.tcpl.mutex.Lock()
	c.p.tcpl.clients[c] = struct{}{}
	c.p.tcpl.mutex.Unlock()

	go c.run()

	return c
}

func (c *serverClient) close() error {
	// already deleted
	if _, ok := c.p.tcpl.clients[c]; !ok {
		c.log("client already deleted")
		return nil
	}

	delete(c.p.tcpl.clients, c)
	c.conn.NetConn().Close()
	close(c.write)

	if len(c.p.tcpl.clients) == 0 {
		c.log("closing path %s", c.path)

		if len(c.p.streams) == 0 {
			c.log("no streams")
			return nil
		}

		str, ok := c.p.streams[c.path]

		if !ok {
			c.log("no stream for path %s", c.path)
			return nil
		} else {

			// delete stream from list
			delete (c.p.streams, c.path)

			if str == nil {
				c.log("stream for path %s not created", c.path)
				return nil
			}

			c.log ("closing stream for path %s", str.path)

			if str.state == _STREAM_STATE_READY {
				c.log ("closing ready stream")
				str.close()
			} else {
				c.log("stream not ready")
			}
		}
	}

	return nil
}

func (c *serverClient) log(format string, args ...interface{}) {
	// keep remote address outside format, since it can contain %
	log.Println("[RTSP client " + c.conn.NetConn().RemoteAddr().String() + "] " +
		fmt.Sprintf(format, args...))
}

func (c *serverClient) ip() net.IP {
	return c.conn.NetConn().RemoteAddr().(*net.TCPAddr).IP
}

func (c *serverClient) zone() string {
	return c.conn.NetConn().RemoteAddr().(*net.TCPAddr).Zone
}

func (c *serverClient) run() {
	c.log("connected")

	for {
		req, err := c.conn.ReadRequest()
		if err != nil {
			if err != io.EOF {
				c.log("Read ERR: %s", err)
			}
			break
		}

		ok := c.handleRequest(req)
		if !ok {
			break
		}
	}

	func() {
		c.log("locking")
		c.p.tcpl.mutex.Lock()
		defer c.p.tcpl.mutex.Unlock()
		c.log("closing")
		c.close()
		c.log("closed")
	}()

	c.log("disconnected")

	close(c.done)
}

func (c *serverClient) writeResError(req *gortsplib.Request, code gortsplib.StatusCode, err error) {
	c.log("WRITE RES ERR: %s", err)

	header := gortsplib.Header{}
	if cseq, ok := req.Header["CSeq"]; ok && len(cseq) == 1 {
		header["CSeq"] = []string{cseq[0]}
	}

	c.conn.WriteResponse(&gortsplib.Response{
		StatusCode: code,
		Header:     header,
	})
}

var errAuthCritical = errors.New("auth critical")
var errAuthNotCritical = errors.New("auth not critical")

func (c *serverClient) validateAuth(req *gortsplib.Request, user string, pass string, auth **gortsplib.AuthServer) error {
	if user == "" {
		return nil
	}

	initialRequest := false
	if *auth == nil {
		initialRequest = true
		*auth = gortsplib.NewAuthServer(user, pass, nil)
	}

	err := (*auth).ValidateHeader(req.Header["Authorization"], req.Method, req.Url)
	if err != nil {
		if !initialRequest {
			c.log("ERR: Unauthorized: %s", err)
		}

		c.conn.WriteResponse(&gortsplib.Response{
			StatusCode: gortsplib.StatusUnauthorized,
			Header: gortsplib.Header{
				"CSeq":             []string{req.Header["CSeq"][0]},
				"WWW-Authenticate": (*auth).GenerateHeader(),
			},
		})

		if !initialRequest {
			return errAuthCritical
		}

		return errAuthNotCritical
	}

	return nil
}

func (c *serverClient) handleRequest(req *gortsplib.Request) bool {
	c.log("Method %s, URL %s", string(req.Method), req.Url.String())

	cseq, ok := req.Header["CSeq"]
	if !ok || len(cseq) != 1 {
		c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("cseq missing"))
		return false
	}

	path := func() string {
		ret := req.Url.Path

		// remove leading slash
		if len(ret) > 0 {
			ret = ret[1:]
		}

		// strip any subpath
		if n := strings.Index(ret, "/"); n >= 0 {
			ret = ret[:n]
		}

		return ret
	}()

	switch req.Method {
	case gortsplib.OPTIONS:
		// do not check state, since OPTIONS can be requested
		// in any state
		c.conn.WriteResponse(&gortsplib.Response{
			StatusCode: gortsplib.StatusOK,
			Header: gortsplib.Header{
				"CSeq": []string{cseq[0]},
				"Public": []string{strings.Join([]string{
					string(gortsplib.DESCRIBE),
					string(gortsplib.ANNOUNCE),
					string(gortsplib.SETUP),
					string(gortsplib.PLAY),
					string(gortsplib.RECORD),
					string(gortsplib.PAUSE),
					string(gortsplib.TEARDOWN),
				}, ", ")},
			},
		})

		return true

	case gortsplib.DESCRIBE, gortsplib.ANNOUNCE:
		if c.state != _CLIENT_STATE_STARTING {
			c.writeResError(req, gortsplib.StatusBadRequest,
				fmt.Errorf("client is in state '%s' instead of '%s'", c.state, _CLIENT_STATE_STARTING))
			return false
		}

		err := c.validateAuth(req, c.p.conf.Server.ReadUser, c.p.conf.Server.ReadPass, &c.readAuth)
		if err != nil {
			if err == errAuthCritical {
				return false
			}
			return true
		}

		clientAnnounce := false
		clientSdpParsed := new(sdp.Message)

		if req.Method == gortsplib.ANNOUNCE {
			contentType, ok := req.Header["Content-Type"]
			if !ok || len(contentType) != 1 {
				c.writeResError(req, gortsplib.StatusBadRequest,
					fmt.Errorf("ERR: Content-Type not provided"))
				return true
			}

			if contentType[0] != "application/sdp" {
				c.writeResError(req, gortsplib.StatusBadRequest,
					fmt.Errorf("ERR: wrong Content-Type, expected application/sdp"))
				return true
			}

			clientSdpParsed, err = gortsplib.SDPParse(req.Content)
			if err != nil {
				c.writeResError(req, gortsplib.StatusBadRequest, err)
				return true
			}

			// note that we're pushing to the server
			clientAnnounce = true
		}

		svrSdpText, err := func() ([]byte, error) {
			c.p.tcpl.mutex.Lock()
			defer c.p.tcpl.mutex.Unlock()

			str, ok := c.p.streams[path]
			if !ok {
				// create new stream
				c.p.streams[path], err = newStream(c.p, req.Url, c.streamProtocol, clientAnnounce, clientSdpParsed)
				if err != nil {
					return nil, fmt.Errorf("unable to create new stream on path '%s'", path)
				}

				c.log("created new stream %s path %s", req.Url.Host, path)

				str, ok = c.p.streams[path]
				if !ok {
					return nil, fmt.Errorf("Unexpected Error")
				}

				// run the stream
				go str.run()
			}

			for i := 0; i < 25; i++ {
				if str.state == _STREAM_STATE_READY {
					c.log("stream %s is ready", path)
					break
				}
				c.log("stream %s is not ready yet", path)

				c.p.tcpl.mutex.Unlock()
				time.Sleep(time.Millisecond)
				c.p.tcpl.mutex.Lock()
			}

			if str.state != _STREAM_STATE_READY {
				return nil, fmt.Errorf("ERR: stream '%s' is not ready", path)
			}

			return str.serverSdpText, nil
		}()
		if err != nil {
			c.writeResError(req, gortsplib.StatusBadRequest, err)
			return false
		}


		if req.Method == gortsplib.ANNOUNCE {
			c.conn.WriteResponse(&gortsplib.Response{
				StatusCode: gortsplib.StatusOK,
				Header: gortsplib.Header{
					"CSeq":         []string{cseq[0]},
				},
			})
		} else {
			c.conn.WriteResponse(&gortsplib.Response{
				StatusCode: gortsplib.StatusOK,
				Header: gortsplib.Header{
					"CSeq":         []string{cseq[0]},
					"Content-Base": []string{req.Url.String() + "/"},
					"Content-Type": []string{"application/sdp"},
				},
				Content: svrSdpText,
			})
		}

		return true

	case gortsplib.SETUP:
		tsRaw, ok := req.Header["Transport"]
		if !ok || len(tsRaw) != 1 {
			c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("transport header missing"))
			return false
		}

		th := gortsplib.ReadHeaderTransport(tsRaw[0])
		if _, ok := th["multicast"]; ok {
			c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("multicast is not supported"))
			return false
		}

		switch c.state {
		// play
		case _CLIENT_STATE_STARTING, _CLIENT_STATE_PRE_PLAY:
			err := c.validateAuth(req, c.p.conf.Server.ReadUser, c.p.conf.Server.ReadPass, &c.readAuth)
			if err != nil {
				if err == errAuthCritical {
					return false
				}
				return true
			}

			// play via UDP
			if func() bool {
				_, ok := th["RTP/AVP"]
				if ok {
					return true
				}
				_, ok = th["RTP/AVP/UDP"]
				if ok {
					return true
				}
				return false
			}() {
				if _, ok := c.p.protocols[_STREAM_PROTOCOL_UDP]; !ok {
					c.writeResError(req, gortsplib.StatusUnsupportedTransport, fmt.Errorf("UDP streaming is disabled"))
					return false
				}

				rtpPort, rtcpPort := th.GetPorts("client_port")
				if rtpPort == 0 || rtcpPort == 0 {
					c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("transport header does not have valid client ports (%s)", tsRaw[0]))
					return false
				}

				if c.path != "" && path != c.path {
					c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("path %s has changed", path))
					return false
				}

				err := func() error {
					c.p.tcpl.mutex.Lock()
					defer c.p.tcpl.mutex.Unlock()

					str, ok := c.p.streams[path]
					if !ok {
						return fmt.Errorf("there is no stream on path %s", path)
					} else if str == nil {
						return fmt.Errorf("stream for path %s not set up yet", path)
					}

					if str.state != _STREAM_STATE_READY {
						return fmt.Errorf("stream '%s' is not ready yet", path)
					}

					if len(c.streamTracks) > 0 && c.streamProtocol != _STREAM_PROTOCOL_UDP {
						return fmt.Errorf("client want to send tracks with different protocols")
					}

					if len(c.streamTracks) >= len(str.serverSdpParsed.Medias) {
						return fmt.Errorf("all the tracks have already been setup")
					}

					c.path = path
					c.streamProtocol = _STREAM_PROTOCOL_UDP
					c.streamTracks = append(c.streamTracks, &track{
						rtpPort:  rtpPort,
						rtcpPort: rtcpPort,
					})

					c.state = _CLIENT_STATE_PRE_PLAY
					return nil
				}()
				if err != nil {
					c.writeResError(req, gortsplib.StatusBadRequest, err)
					return false
				}

				c.conn.WriteResponse(&gortsplib.Response{
					StatusCode: gortsplib.StatusOK,
					Header: gortsplib.Header{
						"CSeq": []string{cseq[0]},
						"Transport": []string{strings.Join([]string{
							"RTP/AVP/UDP",
							"unicast",
							fmt.Sprintf("client_port=%d-%d", rtpPort, rtcpPort),
							fmt.Sprintf("server_port=%d-%d", c.p.conf.Server.RtpPort, c.p.conf.Server.RtcpPort),
						}, ";")},
						"Session": []string{"12345678"},
					},
				})
				return true

				// play via TCP
			} else if _, ok := th["RTP/AVP/TCP"]; ok {
				if _, ok := c.p.protocols[_STREAM_PROTOCOL_TCP]; !ok {
					c.writeResError(req, gortsplib.StatusUnsupportedTransport, fmt.Errorf("TCP streaming is disabled"))
					return false
				}

				if c.path != "" && path != c.path {
					c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("path %s has changed", path))
					return false
				}

				err := func() error {
					c.p.tcpl.mutex.Lock()
					defer c.p.tcpl.mutex.Unlock()

					str, ok := c.p.streams[path]
					if !ok {
						return fmt.Errorf("there is no stream on path '%s'", path)
					}

					if len(c.streamTracks) > 0 && c.streamProtocol != _STREAM_PROTOCOL_TCP {
						return fmt.Errorf("client want to send tracks with different protocols")
					}

					if len(c.streamTracks) >= len(str.serverSdpParsed.Medias) {
						return fmt.Errorf("all the tracks have already been setup")
					}

					c.path = path
					c.streamProtocol = _STREAM_PROTOCOL_TCP
					c.streamTracks = append(c.streamTracks, &track{
						rtpPort:  0,
						rtcpPort: 0,
					})

					c.state = _CLIENT_STATE_PRE_PLAY
					return nil
				}()
				if err != nil {
					c.writeResError(req, gortsplib.StatusBadRequest, err)
					return false
				}

				interleaved := fmt.Sprintf("%d-%d", ((len(c.streamTracks) - 1) * 2), ((len(c.streamTracks)-1)*2)+1)

				c.conn.WriteResponse(&gortsplib.Response{
					StatusCode: gortsplib.StatusOK,
					Header: gortsplib.Header{
						"CSeq": []string{cseq[0]},
						"Transport": []string{strings.Join([]string{
							"RTP/AVP/TCP",
							"unicast",
							fmt.Sprintf("interleaved=%s", interleaved),
						}, ";")},
						"Session": []string{"12345678"},
					},
				})
				return true

			} else {
				c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("transport header does not contain a valid protocol (RTP/AVP, RTP/AVP/UDP or RTP/AVP/TCP) (%s)", tsRaw[0]))
				return false
			}

		default:
			c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("client is in state '%s'", c.state))
			return false
		}

	case gortsplib.PLAY, gortsplib.RECORD:
		if c.state != _CLIENT_STATE_PRE_PLAY {
			c.writeResError(req, gortsplib.StatusBadRequest,
				fmt.Errorf("client is in state '%s' instead of '%s'", c.state, _CLIENT_STATE_PRE_PLAY))
			return false
		}

		if path != c.path {
			// c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("path %s has changed (was %s) for PLAY", path, c.path))
			// return false
			c.log("path %s has changed (was %s)", path, c.path)
		}

		err := func() error {
			c.p.tcpl.mutex.Lock()
			defer c.p.tcpl.mutex.Unlock()

			str, ok := c.p.streams[c.path]
			if !ok {
				return fmt.Errorf("no one is streaming on path %s", c.path)
			} else if str == nil {
				return fmt.Errorf("stream on path %s is not valid", c.path)
			}

			if len(c.streamTracks) != len(str.serverSdpParsed.Medias) {
				return fmt.Errorf("not all tracks have been setup")
			}

			return nil
		}()
		if err != nil {
			c.writeResError(req, gortsplib.StatusBadRequest, err)
			return false
		}

		// first write response, then set state
		// otherwise, in case of TCP connections, RTP packets could be written
		// before the response
		c.conn.WriteResponse(&gortsplib.Response{
			StatusCode: gortsplib.StatusOK,
			Header: gortsplib.Header{
				"CSeq":    []string{cseq[0]},
				"Session": []string{"12345678"},
			},
		})

		c.log("is receiving on path '%s', %d %s via %s", c.path, len(c.streamTracks), func() string {
			if len(c.streamTracks) == 1 {
				return "track"
			}
			return "tracks"
		}(), c.streamProtocol)

		c.p.tcpl.mutex.Lock()
		c.state = _CLIENT_STATE_PLAY
		c.p.tcpl.mutex.Unlock()

		// when protocol is TCP, the RTSP connection becomes a RTP connection
		if c.streamProtocol == _STREAM_PROTOCOL_TCP {
			// write RTP frames sequentially
			go func() {
				for frame := range c.write {
					c.conn.WriteInterleavedFrame(frame)
				}
			}()

			// receive RTP feedback, do not parse it, wait until connection closes
			buf := make([]byte, 2048)
			for {
				_, err := c.conn.NetConn().Read(buf)
				if err != nil {
					if err != io.EOF {
						c.log("Read ERR: %s", err)
					}
					return false
				}
			}
		}

		return true

	case gortsplib.PAUSE:
		if c.state != _CLIENT_STATE_PLAY {
			c.writeResError(req, gortsplib.StatusBadRequest,
				fmt.Errorf("client is in state '%s' instead of '%s'", c.state, _CLIENT_STATE_PLAY))
			return false
		}

		if path != c.path {
			c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("path %s has changed (was %s) for PAUSE", path, c.path))
			return false
		}

		c.log("paused")

		c.p.tcpl.mutex.Lock()
		c.state = _CLIENT_STATE_PRE_PLAY
		c.p.tcpl.mutex.Unlock()

		c.conn.WriteResponse(&gortsplib.Response{
			StatusCode: gortsplib.StatusOK,
			Header: gortsplib.Header{
				"CSeq":    []string{cseq[0]},
				"Session": []string{"12345678"},
			},
		})
		return true

	case gortsplib.TEARDOWN:
		// respond
		c.conn.WriteResponse(&gortsplib.Response{
			StatusCode: gortsplib.StatusOK,
			Header: gortsplib.Header{
				"CSeq":    []string{cseq[0]},
			},
		})

		// close
		return false

	default:
		c.writeResError(req, gortsplib.StatusBadRequest, fmt.Errorf("unhandled method '%s'", req.Method))
		return false
	}
}
